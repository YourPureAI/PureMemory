document.addEventListener('DOMContentLoaded', restoreOptions);
document.getElementById('addBtn').addEventListener('click', addDomain);

function restoreOptions() {
    chrome.storage.sync.get({ offlineBlacklist: [] }, (items) => {
        const list = document.getElementById('blacklist');
        list.innerHTML = '';
        items.offlineBlacklist.forEach(domain => appendToList(domain));
    });
}

function addDomain() {
    const input = document.getElementById('domainInput');
    const domain = input.value.trim().toLowerCase();
    
    if (domain) {
        chrome.storage.sync.get({ offlineBlacklist: [] }, (items) => {
            if (!items.offlineBlacklist.includes(domain)) {
                const updatedList = [...items.offlineBlacklist, domain];
                chrome.storage.sync.set({ offlineBlacklist: updatedList }, () => {
                    appendToList(domain);
                    input.value = '';
                });
            }
        });
    }
}

function appendToList(domain) {
    const list = document.getElementById('blacklist');
    const li = document.createElement('li');
    li.textContent = domain;
    
    const span = document.createElement('span');
    span.textContent = ' ✖';
    span.className = 'remove';
    span.onclick = () => removeDomain(domain);
    
    li.appendChild(span);
    list.appendChild(li);
}

function removeDomain(domain) {
    chrome.storage.sync.get({ offlineBlacklist: [] }, (items) => {
        const updatedList = items.offlineBlacklist.filter(d => d !== domain);
        chrome.storage.sync.set({ offlineBlacklist: updatedList }, restoreOptions);
    });
}
