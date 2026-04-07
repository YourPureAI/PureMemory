package watchers

import (
	"context"
	"encoding/json"
	"log"
	"syscall"
	"time"
	"unsafe"
	"golang.org/x/sys/windows"
)

const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	kbdUser32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW   = kbdUser32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = kbdUser32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = kbdUser32.NewProc("UnhookWindowsHookEx")
	procGetMessageW         = kbdUser32.NewProc("GetMessageW")
)

// KeystrokeWatcher uses native Win32 hooks to aggregate global typing metrics 
// (speed, word counts) without retaining plaintext outside of volatile memory.
type KeystrokeWatcher struct {
	hookHandle uintptr
}

func NewKeystrokeWatcher() *KeystrokeWatcher {
	return &KeystrokeWatcher{}
}

func (w *KeystrokeWatcher) Start(ctx context.Context, out chan<- *Event) {
	log.Println("Global Keystroke Hook initialized (WH_KEYBOARD_LL)")

	sessionStart := time.Now()
	var words int
	var chars int

	// The callback is dispatched on keydown events globally
	hookCallback := syscall.NewCallback(func(nCode int, wParam uintptr, lParam uintptr) uintptr {
		if nCode >= 0 && wParam == WM_KEYDOWN {
			kbdstruct := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
			
			// Simple heuristic accumulation before emitting to Privacy filter.
			// Spacebar indicates a split between words.
			if kbdstruct.VkCode == 0x20 { 
				words++
			} else {
				chars++
			}

			// Batch payload if duration exceeds threshold
			if time.Since(sessionStart) > 10*time.Second {
				if chars > 0 {
					rawPayload, _ := json.Marshal(TextInputPayload{WordCount: words, DurationMs: time.Since(sessionStart).Milliseconds()})
					out <- &Event{
						EventID: "evt_kbd_" + time.Now().Format("20060102150405"),
						TsStart: sessionStart,
						TsEnd:   time.Now(),
						Type:    EventTextInput,
						Context: EventContext{
							OSPlatform: "windows",
						},
						Payload: rawPayload,
					}
				}
				sessionStart = time.Now()
				words = 0
				chars = 0
			}
		}
		
		ret, _, _ := procCallNextHookEx.Call(w.hookHandle, uintptr(nCode), wParam, lParam)
		return ret
	})

	hookRaw, _, err := procSetWindowsHookExW.Call(WH_KEYBOARD_LL, hookCallback, 0, 0)
	if hookRaw == 0 {
		log.Printf("Failed to set windows hook: %v", err)
		return
	}
	w.hookHandle = hookRaw
	defer procUnhookWindowsHookEx.Call(w.hookHandle)

	// Background cleanup listening heavily on Context Cancel
	go func() {
		<-ctx.Done()
		procUnhookWindowsHookEx.Call(w.hookHandle)
	}()

	// Lock the OS Thread and pump messages gracefully to ensure 
	// the Win32 hook doesn't hang the system and captures events synchronously.
	var msg struct {
		Hwnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}

	msgPtr := uintptr(unsafe.Pointer(&msg)) //nolint:unsafeptr
	for {
		ret, _, _ := procGetMessageW.Call(msgPtr, 0, 0, 0)
		if int32(ret) <= 0 || ctx.Err() != nil {
			break
		}
	}
}
