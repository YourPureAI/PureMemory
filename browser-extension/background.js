const DWELL_FULL_SECONDS = 30; // Minimum dwell time before text extraction
const EXTRACTED_TTL_MS = 4 * 60 * 60 * 1000; // 4 hours - matches Go agent TTL

// Persistent dedup store: survives service worker restarts via chrome.storage.session
async function isAlreadyExtracted(url) {
    const data = await chrome.storage.session.get('extractedUrls');
    const store = data.extractedUrls || {};
    const entry = store[url];
    return !!(entry && (Date.now() - entry.ts) < EXTRACTED_TTL_MS);
}

async function markAsExtracted(url) {
    const data = await chrome.storage.session.get('extractedUrls');
    const store = data.extractedUrls || {};
    store[url] = { ts: Date.now() };
    await chrome.storage.session.set({ extractedUrls: store });
}

// Simple in-memory session store (supplementary)
const sessions = {};

chrome.tabs.onActivated.addListener(async ({ tabId }) => {
    // When switching tabs, calculate how much time was spent on the previously focused tabs
    Object.keys(sessions).forEach(id => {
        if (parseInt(id) !== tabId) {
            const s = sessions[id];
            if (s && s.focusStartTs) {
                s.accumulatedDwellMs += (Date.now() - s.focusStartTs);
                s.focusStartTs = null; // Mark as unfocused
            }
            chrome.alarms.clear(`dwell_${id}`);
        }
    });

    // For the newly focused tab, resume its alarm based on remaining time
    const session = sessions[tabId];
    if (session) {
        const extracted = await isAlreadyExtracted(session.url);
        if (!extracted) {
            session.focusStartTs = Date.now();
            let remainingMs = (DWELL_FULL_SECONDS * 1000) - session.accumulatedDwellMs;
            if (remainingMs <= 0) remainingMs = 100; // tiny delay to trigger immediately

            chrome.alarms.clear(`dwell_${tabId}`);
            chrome.alarms.create(`dwell_${tabId}`, { delayInMinutes: remainingMs / 60000 });
            console.log(`[UserMemory] Tab ${tabId} re-activated. Remaining dwell: ${Math.round(remainingMs/1000)}s for ${session.url}`);
        }
    }
});

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
    if (changeInfo.status !== 'complete') return;
    if (!tab.url) return;
    
    if (tab.url.startsWith("chrome://") ||
        tab.url.startsWith("edge://") ||
        tab.url.startsWith("about:")) return;

    chrome.alarms.clear(`dwell_${tabId}`);
    
    sessions[tabId] = {
        url: tab.url,
        title: tab.title,
        accumulatedDwellMs: 0,
        focusStartTs: Date.now()
    };

    chrome.alarms.create(`dwell_${tabId}`, { delayInMinutes: DWELL_FULL_SECONDS / 60 });
    console.log(`[UserMemory] Tab ${tabId} navigation started: ${tab.url}`);
});

// All logic is fully async — no nested callback hell, no await-in-non-async errors
chrome.alarms.onAlarm.addListener(async (alarm) => {
    if (!alarm.name.startsWith('dwell_')) return;
    
    const tabId = parseInt(alarm.name.split('_')[1]);
    const session = sessions[tabId];
    if (!session) return;

    // Verify tab is still active and on the same URL
    let tab;
    try {
        tab = await chrome.tabs.get(tabId);
    } catch {
        return; // Tab was closed
    }
    if (!tab.active) return;
    if (tab.url !== session.url) return;

    // Primary dedup check — survives service worker restarts
    if (await isAlreadyExtracted(tab.url)) {
        console.log(`[UserMemory] Skipping duplicate extraction: ${tab.url}`);
        return;
    }

    console.log(`[UserMemory] Dwell threshold reached for: ${tab.url}`);

    try {
        // Step 1: Inject Readability library
        await chrome.scripting.executeScript({ target: { tabId }, files: ['readability.min.js'] });
        
        // Step 2: Inject extraction function
        await chrome.scripting.executeScript({ target: { tabId }, files: ['content-script.js'] });

        // Step 3: Run extraction and collect result
        const results = await chrome.scripting.executeScript({
            target: { tabId },
            func: () => typeof window.extractReadableContent === 'function'
                ? window.extractReadableContent()
                : null
        });

        const result = results && results[0] && results[0].result;
        if (result) {
            await markAsExtracted(session.url);
            
            let totalDwell = session.accumulatedDwellMs;
            if (session.focusStartTs) {
                totalDwell += (Date.now() - session.focusStartTs);
            }
            
            console.log(`[UserMemory] Extracted ${result.word_count} words. Sending to agent.`);
            sendToAgent({
                url: session.url,
                title: session.title,
                dwell_ms: totalDwell,
                web_content: result
            });
        } else {
            console.warn('[UserMemory] Readability returned no content for this page.');
        }
    } catch (err) {
        console.warn('[UserMemory] Extraction pipeline error:', err.message);
    }
});

function sendToAgent(data) {
    chrome.storage.sync.get({ offlineBlacklist: [] }, (items) => {
        const isBlacklisted = items.offlineBlacklist.some(domain => data.url.includes(domain));
        if (isBlacklisted) {
            console.log('[UserMemory] Domain blacklisted, skipping.');
            return;
        }

        fetch('http://localhost:45678/extension-event', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        })
        .then(r => console.log('[UserMemory] Agent acknowledged:', r.status))
        .catch(err => console.warn('[UserMemory] Agent not reachable:', err.message));
    });
}
