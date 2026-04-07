package watchers

import (
	"context"
	"encoding/json"
	"log"
	"time"
	"unsafe"
	"golang.org/x/sys/windows"
)

const (
	CF_UNICODETEXT = 13
)

var (
	clipUser32                         = windows.NewLazySystemDLL("user32.dll")
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard              = clipUser32.NewProc("OpenClipboard")
	procGetClipboardData           = clipUser32.NewProc("GetClipboardData")
	procCloseClipboard             = clipUser32.NewProc("CloseClipboard")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGetClipboardSequenceNumber = clipUser32.NewProc("GetClipboardSequenceNumber")
)

// ClipboardWatcher polls the Win32 clipboard API strictly evaluating for textual data.
type ClipboardWatcher struct {}

func NewClipboardWatcher() *ClipboardWatcher {
	return &ClipboardWatcher{}
}

func (w *ClipboardWatcher) Start(ctx context.Context, out chan<- *Event) {
	log.Println("Clipboard Polling Watcher initialized (GetClipboardSequenceNumber)")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Track sequence number instead of hashing strings. O(1) performance.
	var lastSeqNum uint32

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Cheap Win32 API call to verify if clipboard mutated since last tick
			currentSeqNum, _, _ := procGetClipboardSequenceNumber.Call()
			if uint32(currentSeqNum) == lastSeqNum || currentSeqNum == 0 {
				continue
			}
			lastSeqNum = uint32(currentSeqNum)

			// Fast Lock Clipboard
			openRet, _, _ := procOpenClipboard.Call(0)
			if openRet == 0 {
				continue // Clipboard currently locked by another app
			}

			// Request purely Unicode text format
			dataHandle, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
			if dataHandle != 0 {
				lockedPtr, _, _ := procGlobalLock.Call(dataHandle)
				if lockedPtr != 0 {
					// Cast to *uint16 via a local uintptr variable to satisfy Go vet
					addr := lockedPtr
					text := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(addr))) //nolint:unsafeptr
					procGlobalUnlock.Call(dataHandle)
					
					wordCount := len(text) // simplified, usually split by spaces
					if wordCount > 0 {
						rawPayload, _ := json.Marshal(ClipboardPayload{ContentType: "text", Text: text, CharCount: len(text)})
						out <- &Event{
							EventID: "evt_clip_" + time.Now().Format("20060102150405"),
							TsStart: time.Now(),
							Type:    EventClipboard,
							Context: EventContext{
								OSPlatform: "windows",
							},
							Payload: rawPayload,
						}
					}
				}
			}
			procCloseClipboard.Call()
		}
	}
}
