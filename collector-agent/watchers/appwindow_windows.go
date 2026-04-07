package watchers

import (
	"context"
	"time"
	"unsafe"
	"golang.org/x/sys/windows"
)

var (
	appUser32               = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow = appUser32.NewProc("GetForegroundWindow")
	procGetWindowTextW   = appUser32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = appUser32.NewProc("GetWindowThreadProcessId")
)

type AppWatcher struct {}

func NewAppWatcher() *AppWatcher {
	return &AppWatcher{}
}

func (w *AppWatcher) Start(ctx context.Context, out chan<- *Event) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastWindow windows.HWND

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hwnd, _, _ := procGetForegroundWindow.Call()
			currentHwnd := windows.HWND(hwnd)
			
			if currentHwnd != lastWindow && currentHwnd != 0 {
				lastWindow = currentHwnd
				
				title := getWindowText(currentHwnd)
				
				var pid uint32
				procGetWindowThreadProcessId.Call(uintptr(currentHwnd), uintptr(unsafe.Pointer(&pid)))
				
				// Extract process generic name (App bundle equivalent on Windows)
				out <- &Event{
					EventID: "event_" + time.Now().Format("20060102150405"),
					TsStart: time.Now(),
					Type:    EventAppFocus,
					Context: EventContext{
						AppBundle:   "PID_" + formatPid(pid), // Simplified App name via PID
						WindowTitle: title,
						OSPlatform:  "windows",
					},
				}
			}
		}
	}
}

func getWindowText(hwnd windows.HWND) string {
	b := make([]uint16, 256)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return windows.UTF16ToString(b)
}

func formatPid(pid uint32) string {
	// In production, OpenProcess and QueryFullProcessImageNameW would be used here.
	return "ProcessID"
}
