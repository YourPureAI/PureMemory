package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"user-memory-collector/buffer"
	"user-memory-collector/extension"
	"user-memory-collector/privacy"
	"user-memory-collector/settings"
	"user-memory-collector/transport"
	"user-memory-collector/watchers"
)

// Agent coordinates data collection from watchers, runs privacy filters, and flushes to buffer.
type Agent struct {
	userID         string
	deviceID       string
	sessionID      string
	apiKey         string
	buf            *buffer.SQLiteBuffer
	privacyFilter  *privacy.Filter
	watchersCtx    context.Context
	watchersCancel context.CancelFunc
	wg             sync.WaitGroup
	eventChan      chan *watchers.Event
	lastAppCtx     watchers.EventContext
	appCtxMu       sync.RWMutex
	paused         atomic.Bool // true = events discarded (collection paused by user)
}

// Pause stops buffering new events without stopping OS watchers
func (a *Agent) Pause() {
	a.paused.Store(true)
	log.Println("[Agent] Collection paused")
}

// Resume re-enables event buffering
func (a *Agent) Resume() {
	a.paused.Store(false)
	log.Println("[Agent] Collection resumed")
}

// IsPaused returns current pause state
func (a *Agent) IsPaused() bool {
	return a.paused.Load()
}

// NewAgent initializes the agent with configuration and storage paths.
func NewAgent(dbPath string) (*Agent, error) {
	apiKey := "dummy-api-key-replace-me"

	buf, err := buffer.NewSQLiteBuffer(dbPath)
	if err != nil {
		return nil, err
	}

	filter := privacy.NewFilter("privacy.json")
	
	uid, did := filter.GetIdentity()
	if uid == "" || did == "" {
		if did == "" {
			machineID := "windows-local-machine"
			h := hmac.New(sha256.New, []byte(apiKey))
			h.Write([]byte(machineID))
			did = hex.EncodeToString(h.Sum(nil))
		}
		if uid == "" {
			uid = generateUUID()
		}
		// Save generated defaults back
		_, _, pref, dom, tags := filter.GetConfig()
		filter.SetConfig(uid, did, pref, dom, tags)
	}

	sessionID := generateUUID()
	ctx, cancel := context.WithCancel(context.Background())

	return &Agent{
		userID:         uid,
		deviceID:       did,
		sessionID:      sessionID,
		apiKey:         apiKey,
		buf:            buf,
		privacyFilter:  filter,
		watchersCtx:    ctx,
		watchersCancel: cancel,
		eventChan:      make(chan *watchers.Event, 500),
	}, nil
}

// Start begins all watcher systems on background threads.
func (a *Agent) Start() error {
	log.Println("Starting collector agent on", runtime.GOOS)
	
	// Start the main event processor
	a.wg.Add(1)
	go a.processEvents()

	// Initialize Windows Watchers
	
	// Active window polling/hook
	appWatcher := watchers.NewAppWatcher()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		appWatcher.Start(a.watchersCtx, a.eventChan)
	}()

	// Global Keystroke hook (WH_KEYBOARD_LL)
	// DISABLED (privacy concerns: currently only tracks counts, pending future redesign to safely correlate text with apps)
	/*
	kbWatcher := watchers.NewKeystrokeWatcher()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		// LockOSThread is critical for SetWindowsHookEx to maintain the message loop
		runtime.LockOSThread()
		kbWatcher.Start(a.watchersCtx, a.eventChan)
	}()
	*/

	// Clipboard polling watcher
	cbWatcher := watchers.NewClipboardWatcher()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		cbWatcher.Start(a.watchersCtx, a.eventChan)
	}()

	// Quick Note overlay (Ctrl+Shift+Space) — native Win32 window, zero idle CPU
	qnWatcher := watchers.NewQuickNoteWatcher(a.privacyFilter.RedactText, a.privacyFilter.GetTags)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		qnWatcher.Start(a.watchersCtx, a.eventChan)
	}()

	// System Tray Icon — Pause/Resume/Stop controls, zero idle CPU
	tray := watchers.NewTrayIcon(watchers.TrayCallbacks{
		TogglePause: func() {
			if a.IsPaused() {
				a.Resume()
			} else {
				a.Pause()
			}
		},
		IsPaused: a.IsPaused,
		Stop:     a.Stop,
	})
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		tray.Start(a.watchersCtx)
	}()

	// Background Extension Broker — pause-aware
	extServer := extension.NewServer(a.eventChan, a.IsPaused)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		extServer.Start()
	}()
	
	// Ensure Extension Server stops cleanly when Context dies
	go func() {
		<-a.watchersCtx.Done()
		extServer.Stop()
	}()

	// Settings UI on http://localhost:45679 — zero-cost when idle
	settingsSrv := settings.NewServer(a.privacyFilter, a.buf)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		settingsSrv.Start()
	}()
	go func() {
		<-a.watchersCtx.Done()
		settingsSrv.Stop()
	}()

	// Network Transport Client
	tr := transport.NewClient("http://127.0.0.1:8443", a.apiKey, a.userID, a.deviceID, a.buf)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		tr.StartLoop(a.watchersCtx)
	}()

	// Filesystem Watcher — watches Documents, Desktop, Downloads
	// Passes a callback so the watcher can tag events with the current active app
	fsWatcher := watchers.NewFilesystemWatcher(func() watchers.EventContext {
		a.appCtxMu.RLock()
		defer a.appCtxMu.RUnlock()
		return a.lastAppCtx
	})
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		fsWatcher.Start(a.watchersCtx, a.eventChan)
	}()

	return nil
}

// Stop cleanly shuts down all watchers and flushes the buffer.
func (a *Agent) Stop() {
	log.Println("Stopping agent...")
	a.watchersCancel()
	close(a.eventChan)
	a.wg.Wait()
	
	a.buf.Close()
	log.Println("Agent stopped.")
}

func (a *Agent) processEvents() {
	defer a.wg.Done()
	for ev := range a.eventChan {
		ev.UserID = a.userID
		ev.DeviceID = a.deviceID
		ev.SessionID = a.sessionID

		// Keep lastAppCtx fresh for filesystem watcher correlation
		if ev.Type == watchers.EventAppFocus {
			a.appCtxMu.Lock()
			a.lastAppCtx = ev.Context
			a.appCtxMu.Unlock()
		}

		// Drop events when paused — except QuickNote which always saves
		if a.paused.Load() && ev.Type != watchers.EventQuickNote {
			continue
		}

		if a.privacyFilter.ShouldDiscard(ev) {
			continue
		}

		// Apply prefix-based secret redaction to text-bearing event types.
		// Runs AFTER discard check so we don't waste cycles on dropped events.
		switch ev.Type {
		case watchers.EventTextInput, watchers.EventClipboard,
			watchers.EventFileAccess, watchers.EventURLVisit, watchers.EventQuickNote:
			if len(ev.Payload) > 0 {
				redacted := a.privacyFilter.RedactText(string(ev.Payload))
				ev.Payload = []byte(redacted)
			}
		}

		err := a.buf.SaveEvent(ev)
		if err != nil {
			log.Printf("Failed to buffer event %s: %v\n", ev.Type, err)
		}
	}
}

func generateUUID() string {
	// Dummy UUID for barebones agent demo. In prod use an actual UUID library.
	return "UUID-MOCK-" + time.Now().Format("20060102150405")
}
