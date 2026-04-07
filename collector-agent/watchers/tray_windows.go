package watchers

import (
	"context"
	"log"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── Win32 procs for system tray ───────────────────────────────────────────────
var (
	trayShell32 = windows.NewLazySystemDLL("shell32.dll")
	trayUser32  = windows.NewLazySystemDLL("user32.dll")
	trayKernel  = windows.NewLazySystemDLL("kernel32.dll")

	procShellNotifyIconW  = trayShell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu   = trayUser32.NewProc("CreatePopupMenu")
	procAppendMenuW       = trayUser32.NewProc("AppendMenuW")
	procTrackPopupMenu    = trayUser32.NewProc("TrackPopupMenu")
	procDestroyMenu       = trayUser32.NewProc("DestroyMenu")
	procRegisterClassExW2 = trayUser32.NewProc("RegisterClassExW")
	procCreateWindowExW2  = trayUser32.NewProc("CreateWindowExW")
	procDefWindowProcW2   = trayUser32.NewProc("DefWindowProcW")
	procGetMessageW3      = trayUser32.NewProc("GetMessageW")
	procDispatchMessageW2 = trayUser32.NewProc("DispatchMessageW")
	procTranslateMessage2 = trayUser32.NewProc("TranslateMessage")
	procPostQuitMessage2  = trayUser32.NewProc("PostQuitMessage")
	procGetCursorPos      = trayUser32.NewProc("GetCursorPos")
	procSetForegroundWin  = trayUser32.NewProc("SetForegroundWindow")
	procLoadIconW         = trayUser32.NewProc("LoadIconW")
	procGetModuleHandle2  = trayKernel.NewProc("GetModuleHandleW")
	procPostThreadMsg2    = trayUser32.NewProc("PostThreadMessageW")
	procGetCurrentThrdId2 = trayKernel.NewProc("GetCurrentThreadId")
)

// ── Constants ──────────────────────────────────────────────────────────────────
const (
	nimAdd    = uintptr(0x00000000)
	nimDelete = uintptr(0x00000002)
	nimModify = uintptr(0x00000001)

	nifMessage = uint32(0x00000001)
	nifIcon    = uint32(0x00000002)
	nifTip     = uint32(0x00000004)

	// WM_APP+1: our private tray callback message
	wmTrayCallback = uintptr(0x8001)
	wmQuit2        = uintptr(0x0012)

	// Tray mouse events (sent in LPARAM of wmTrayCallback)
	wmRButtonUp     = uintptr(0x0205)
	wmLButtonDblClk = uintptr(0x0203)

	// Menu item IDs
	menuPause    = uintptr(1)
	menuStop     = uintptr(2)
	menuSettings = uintptr(3)

	// AppendMenuW flags
	mfString    = uintptr(0x0000)
	mfSeparator = uintptr(0x0800)

	// TrackPopupMenu flags — return selected cmd, right-click alignment
	tpmRightButton = uintptr(0x0002)
	tpmReturncmd   = uintptr(0x0100)

	idiInformation = uintptr(32516) // IDI_INFORMATION stock icon
)

// ── NOTIFYICONDATA ─────────────────────────────────────────────────────────────
// Full 976-byte struct matching Windows x64 layout.
// Go's natural alignment rules match C's for these field types.
type notifyIconData struct {
	CbSize           uint32
	// Go inserts 4 bytes padding here to align HWnd to 8 bytes (matches C)
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	// Go inserts 4 bytes padding here to align HIcon to 8 bytes (matches C)
	HIcon            uintptr
	SzTip            [128]uint16 // 256 bytes
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16 // 512 bytes
	UVersion         uint32
	SzInfoTitle      [64]uint16  // 128 bytes
	DwInfoFlags      uint32
	GuidItem         [16]byte
	// Go inserts padding to align HBalloonIcon
	HBalloonIcon     uintptr
}

// ── Window class structs ───────────────────────────────────────────────────────

type trayWNDCLASSEX struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   uintptr
	ClassName  uintptr
	IconSm     uintptr
}

type trayMSG struct {
	Hwnd      uintptr
	Message   uint32
	WParam    uintptr
	LParam    uintptr
	Time      uint32
	PtX, PtY int32
}

type trayPoint struct{ X, Y int32 }

// ── TrayIcon ────────────────────────────────────────────────────────────────────

// TrayCallbacks holds the action callbacks for menu items
type TrayCallbacks struct {
	TogglePause func()
	IsPaused    func() bool
	Stop        func()
}

// TrayIcon manages the Windows notification area icon and context menu
type TrayIcon struct {
	cb    TrayCallbacks
	hwnd  uintptr
	hIcon uintptr
	nid   notifyIconData
}

// trayActive is a package-level reference for the WndProc callback
var trayActive *TrayIcon

func NewTrayIcon(cb TrayCallbacks) *TrayIcon {
	return &TrayIcon{cb: cb}
}

// Start runs the tray on a dedicated locked OS thread
func (t *TrayIcon) Start(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	trayActive = t

	hInst, _, _ := procGetModuleHandle2.Call(0)

	// Register minimal window class
	className, _ := windows.UTF16PtrFromString("PureMemoryTrayWnd")
	wndProcCB := syscall.NewCallback(trayWndProc)

	wc := trayWNDCLASSEX{
		WndProc:   wndProcCB,
		Instance:  hInst,
		ClassName: uintptr(unsafe.Pointer(className)),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	ret, _, err := procRegisterClassExW2.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 {
		log.Printf("[Tray] RegisterClassExW failed: %v", err)
		return
	}

	// Create a hidden WS_POPUP window (guaranteed to work, invisible, zero size)
	winTitle, _ := windows.UTF16PtrFromString("PureMemoryTray")
	t.hwnd, _, err = procCreateWindowExW2.Call(
		0,                                   // no extended style
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winTitle)),
		0x80000000,                          // WS_POPUP (hidden, no title bar)
		0xFFFFFFFF80000000,                  // x = off-screen (-32768)
		0xFFFFFFFF80000000,                  // y = off-screen (-32768)
		1, 1,                                // 1x1 pixel
		0, 0, hInst, 0,
	)
	if t.hwnd == 0 {
		log.Printf("[Tray] CreateWindowExW failed: %v", err)
		return
	}

	// Load stock info icon (blue "i") — no embedded resources needed
	t.hIcon, _, _ = procLoadIconW.Call(0, idiInformation)
	if t.hIcon == 0 {
		log.Printf("[Tray] Warning: LoadIconW returned null, using fallback")
		t.hIcon, _, _ = procLoadIconW.Call(0, uintptr(32512)) // IDI_APPLICATION
	}

	// Build NOTIFYICONDATA and register the tray icon
	tipText, _ := windows.UTF16PtrFromString("PureMemory — Active")
	t.nid = notifyIconData{
		HWnd:             t.hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: uint32(wmTrayCallback),
		HIcon:            t.hIcon,
	}
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	copyTip(&t.nid.SzTip, tipText)

	r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	if r == 0 {
		log.Printf("[Tray] Shell_NotifyIconW(NIM_ADD) failed — icon will not appear")
		return
	}
	log.Printf("[Tray] System tray icon active (hwnd=0x%X, cbSize=%d)", t.hwnd, t.nid.CbSize)

	// Unregister on context cancel
	threadID, _, _ := procGetCurrentThrdId2.Call()
	go func() {
		<-ctx.Done()
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
		procPostThreadMsg2.Call(threadID, wmQuit2, 0, 0)
	}()

	// Message loop — GetMessage blocks on OS, 0% CPU idle
	var msg trayMSG
	for {
		ret, _, _ := procGetMessageW3.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage2.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW2.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// trayWndProc is the window procedure for the tray message window
func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmTrayCallback {
		event := lParam & 0xFFFF
		if event == wmRButtonUp || event == wmLButtonDblClk {
			if trayActive != nil {
				trayActive.showMenu(hwnd)
			}
			return 0
		}
	}
	if msg == uintptr(0x0002) { // WM_DESTROY
		procPostQuitMessage2.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW2.Call(hwnd, msg, wParam, lParam)
	return ret
}

func (t *TrayIcon) showMenu(hwnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()

	sep, _ := windows.UTF16PtrFromString("")

	pauseLabel := "⏸  Pause Collection"
	if t.cb.IsPaused != nil && t.cb.IsPaused() {
		pauseLabel = "▶  Resume Collection"
	}
	pLabel, _ := windows.UTF16PtrFromString(pauseLabel)
	procAppendMenuW.Call(hMenu, mfString, menuPause, uintptr(unsafe.Pointer(pLabel)))

	procAppendMenuW.Call(hMenu, mfSeparator, 0, uintptr(unsafe.Pointer(sep)))

	sLabel, _ := windows.UTF16PtrFromString("⚙  Settings")
	procAppendMenuW.Call(hMenu, mfString, menuSettings, uintptr(unsafe.Pointer(sLabel)))

	procAppendMenuW.Call(hMenu, mfSeparator, 0, uintptr(unsafe.Pointer(sep)))

	stLabel, _ := windows.UTF16PtrFromString("■  Stop Agent")
	procAppendMenuW.Call(hMenu, mfString, menuStop, uintptr(unsafe.Pointer(stLabel)))

	var pt trayPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWin.Call(hwnd)

	cmd, _, _ := procTrackPopupMenu.Call(
		hMenu, tpmRightButton|tpmReturncmd,
		uintptr(pt.X), uintptr(pt.Y),
		0, hwnd, 0,
	)
	procDestroyMenu.Call(hMenu)

	switch cmd {
	case menuPause:
		if t.cb.TogglePause != nil {
			t.cb.TogglePause()
			t.refreshTooltip()
		}
	case menuSettings:
		openSettingsUI()
	case menuStop:
		log.Println("[Tray] Stop requested via tray menu")
		if t.cb.Stop != nil {
			go t.cb.Stop()
		}
	}
}

func (t *TrayIcon) refreshTooltip() {
	tipStr := "PureMemory — Active"
	if t.cb.IsPaused != nil && t.cb.IsPaused() {
		tipStr = "PureMemory — ⏸ Paused"
	}
	tip, _ := windows.UTF16PtrFromString(tipStr)
	copyTip(&t.nid.SzTip, tip)
	t.nid.UFlags = nifTip
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
	t.nid.UFlags = nifMessage | nifIcon | nifTip
}

// openSettingsUI opens the settings page in the default browser
func openSettingsUI() {
	shell32Open := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")
	urlPtr, _ := windows.UTF16PtrFromString("http://127.0.0.1:45679")
	opPtr, _ := windows.UTF16PtrFromString("open")
	shell32Open.Call(0, uintptr(unsafe.Pointer(opPtr)), uintptr(unsafe.Pointer(urlPtr)), 0, 0, 1)
	log.Println("[Tray] Settings UI opened in browser")
}

// copyTip copies a UTF-16 string into the fixed [128]uint16 SzTip array
func copyTip(dst *[128]uint16, src *uint16) {
	if src == nil {
		return
	}
	s := (*[128]uint16)(unsafe.Pointer(src))
	for i := 0; i < 128; i++ {
		dst[i] = s[i]
		if s[i] == 0 {
			break
		}
	}
}
