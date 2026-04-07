package watchers

import (
	"context"
	"encoding/json"
	"log"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── Win32 API procs (prefixed qn to avoid collision) ─────────────────────────
var (
	qnUser32 = windows.NewLazySystemDLL("user32.dll")
	qnGdi32  = windows.NewLazySystemDLL("gdi32.dll")
	qnKern32 = windows.NewLazySystemDLL("kernel32.dll")
	qnShell32 = windows.NewLazySystemDLL("shell32.dll")

	qnDragQueryFileW = qnShell32.NewProc("DragQueryFileW")
	qnOpenClipboard              = qnUser32.NewProc("OpenClipboard")
	qnCloseClipboard             = qnUser32.NewProc("CloseClipboard")
	qnGetClipboardData           = qnUser32.NewProc("GetClipboardData")
	qnIsClipboardFormatAvailable = qnUser32.NewProc("IsClipboardFormatAvailable")
	qnGlobalLock   = qnKern32.NewProc("GlobalLock")
	qnGlobalUnlock = qnKern32.NewProc("GlobalUnlock")

	qnRegisterClassExW       = qnUser32.NewProc("RegisterClassExW")
	qnCreateWindowExW        = qnUser32.NewProc("CreateWindowExW")
	qnShowWindow             = qnUser32.NewProc("ShowWindow")
	qnSetFocus               = qnUser32.NewProc("SetFocus")
	qnGetWindowTextW         = qnUser32.NewProc("GetWindowTextW")
	qnSetWindowTextW         = qnUser32.NewProc("SetWindowTextW")
	qnRegisterHotKey         = qnUser32.NewProc("RegisterHotKey")
	qnUnregisterHotKey       = qnUser32.NewProc("UnregisterHotKey")
	qnSetWindowsHookExW      = qnUser32.NewProc("SetWindowsHookExW")
	qnCallNextHookEx         = qnUser32.NewProc("CallNextHookEx")
	qnUnhookWindowsHookEx    = qnUser32.NewProc("UnhookWindowsHookEx")
	qnGetAsyncKeyState       = qnUser32.NewProc("GetAsyncKeyState")
	qnDefWindowProcW         = qnUser32.NewProc("DefWindowProcW")
	qnGetMessageW2           = qnUser32.NewProc("GetMessageW")
	qnTranslateMessage       = qnUser32.NewProc("TranslateMessage")
	qnDispatchMessageW       = qnUser32.NewProc("DispatchMessageW")
	qnGetSystemMetrics       = qnUser32.NewProc("GetSystemMetrics")
	qnPostQuitMessage        = qnUser32.NewProc("PostQuitMessage")
	qnSetLayeredWindowAttrib = qnUser32.NewProc("SetLayeredWindowAttributes")
	qnSendMessageW           = qnUser32.NewProc("SendMessageW")
	qnLoadCursorW            = qnUser32.NewProc("LoadCursorW")
	qnPostThreadMessageW     = qnUser32.NewProc("PostThreadMessageW")
	qnBeginPaint             = qnUser32.NewProc("BeginPaint")
	qnEndPaint               = qnUser32.NewProc("EndPaint")
	qnDrawTextW              = qnUser32.NewProc("DrawTextW")
	qnInvalidateRect         = qnUser32.NewProc("InvalidateRect")

	qnCreateSolidBrush = qnGdi32.NewProc("CreateSolidBrush")
	qnSetBkColor       = qnGdi32.NewProc("SetBkColor")
	qnSetTextColor     = qnGdi32.NewProc("SetTextColor")
	qnCreateFontW      = qnGdi32.NewProc("CreateFontW")
	qnDeleteObject     = qnGdi32.NewProc("DeleteObject")
	qnRoundRect        = qnGdi32.NewProc("RoundRect")
	qnSelectObject     = qnGdi32.NewProc("SelectObject")
	qnCreatePen        = qnGdi32.NewProc("CreatePen")
	qnSetBkMode        = qnGdi32.NewProc("SetBkMode")

	qnGetCurrentThreadId = qnKern32.NewProc("GetCurrentThreadId")
	qnGetModuleHandleW   = qnKern32.NewProc("GetModuleHandleW")
)

// ── Constants ──────────────────────────────────────────────────────────────────
const (
	qnWmHotkey       = uintptr(0x0312)
	qnWmKeydown      = uintptr(0x0100)
	qnWmCtlColorEdit = uintptr(0x0133)
	qnWmSetFont      = uintptr(0x0030)
	qnWmDestroy      = uintptr(0x0002)
	qnWmQuit         = uintptr(0x0012)
	qnWmPaint        = uintptr(0x000F)
	qnWmLButtonDown  = uintptr(0x0201)

	qnCfUnicodeText = 13
	qnCfHDrop       = 15

	qnVkReturn       = uintptr(0x0D)
	qnVkEscape       = uintptr(0x1B)
	qnSwShow         = uintptr(5)
	qnSwHide         = uintptr(0)
	qnHotKeyID       = uintptr(1)
	qnModCtrl        = uintptr(0x0002)
	qnModShift       = uintptr(0x0004)
	qnLwaAlpha       = uintptr(0x0002)
	qnEmSetCueBanner = uintptr(0x1501)

	qnDtCenter     = uintptr(0x01)
	qnDtVCenter    = uintptr(0x04)
	qnDtSingleLine = uintptr(0x20)
	qnPsNull       = uintptr(5)

	// COLORREF in BGR format
	qnColorBg          = uintptr(0x00302C2C) // #2C2C30
	qnColorText        = uintptr(0x00F0F0F0) // #F0F0F0
	qnColorBtnActive   = uintptr(0x00F16663) // #6366F1 (Indigo)
	qnColorBtnInactive = uintptr(0x003C2828) // #28283C (Dark Slate)
	qnColorTextMuted   = uintptr(0x00A0A0A0)

	// Overlay dimensions
	qnWidth  = 700
	qnHeight = 135
	qnYInset = 60
)

// ── Structs ───────────────────────────────────────────────────────────────────

type qnKBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type qnMSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	PtX, PtY int32
}

type qnWNDCLASSEX struct {
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

type qnPAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     qnRECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type qnRECT struct {
	Left, Top, Right, Bottom int32
}

type qnButton struct {
	ID    int
	Label *uint16
	Rect  qnRECT
	Type  string
}

// ── Watcher ───────────────────────────────────────────────────────────────────

type QuickNoteWatcher struct {
	out          chan<- *Event
	mainHwnd     uintptr
	editHwnd     uintptr
	bgBrush      uintptr
	editFont     uintptr
	btnFont      uintptr
	visible      bool
	activeBtn    int
	contextBtns  []qnButton
	tagBtns      []qnButton
	selectedTags map[int]bool
	redactor     func(string) string
	getTags      func() []string
}

var qnActive *QuickNoteWatcher

func NewQuickNoteWatcher(redact func(string) string, getTags func() []string) *QuickNoteWatcher {
	return &QuickNoteWatcher{redactor: redact, getTags: getTags, selectedTags: make(map[int]bool)}
}

func (qn *QuickNoteWatcher) Start(ctx context.Context, out chan<- *Event) {
	qn.out = out
	qnActive = qn

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// ── GDI Resources ──────────────────────────────────────────────────────────
	qn.bgBrush, _, _ = qnCreateSolidBrush.Call(qnColorBg)

	fontNameW, _ := windows.UTF16PtrFromString("Segoe UI")
	qn.editFont, _, _ = qnCreateFontW.Call(
		20, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(fontNameW)),
	)
	qn.btnFont, _, _ = qnCreateFontW.Call(
		15, 0, 0, 0, 500, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(fontNameW)),
	)

	// ── Buttons Layout ─────────────────────────────────────────────────────────
	str1, _ := windows.UTF16PtrFromString("Standard Message")
	str2, _ := windows.UTF16PtrFromString("Clipboard Message")
	str3, _ := windows.UTF16PtrFromString("File Message")

	// 145px width each, 8px gaps
	qn.contextBtns = []qnButton{
		{0, str1, qnRECT{14, 10, 154, 34}, "standard"},
		{1, str2, qnRECT{162, 10, 302, 34}, "clipboard"},
		{2, str3, qnRECT{310, 10, 450, 34}, "file"},
	}
	qn.activeBtn = 0

	hInst, _, _ := qnGetModuleHandleW.Call(0)

	// ── Window Class ───────────────────────────────────────────────────────────
	className, _ := windows.UTF16PtrFromString("PureMemoryNote")
	wndProcCB := syscall.NewCallback(qnWindowProc)

	wc := qnWNDCLASSEX{
		Style:      0x0003,
		WndProc:    wndProcCB,
		Instance:   hInst,
		Background: qn.bgBrush,
		ClassName:  uintptr(unsafe.Pointer(className)),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.Cursor, _, _ = qnLoadCursorW.Call(0, 32512)

	qnRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// ── Parent Window ─────────────────────────────────────────────────────────
	screenW, _, _ := qnGetSystemMetrics.Call(0)
	x := (int(screenW) - qnWidth) / 2

	windowTitle, _ := windows.UTF16PtrFromString("")
	qn.mainHwnd, _, _ = qnCreateWindowExW.Call(
		0x00080088, // WS_EX_TOPMOST|WS_EX_LAYERED|WS_EX_TOOLWINDOW
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowTitle)),
		0x80000000, // WS_POPUP
		uintptr(x), uintptr(qnYInset), uintptr(qnWidth), uintptr(qnHeight),
		0, 0, hInst, 0,
	)
	qnSetLayeredWindowAttrib.Call(qn.mainHwnd, 0, 245, qnLwaAlpha)

	// ── EDIT Control ──────────────────────────────────────────────────────────
	editClass, _ := windows.UTF16PtrFromString("EDIT")
	qn.editHwnd, _, _ = qnCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(editClass)),
		0,
		0x50000080, // WS_CHILD|WS_VISIBLE|ES_AUTOHSCROLL
		uintptr(14), uintptr(44), uintptr(qnWidth-28), uintptr(38),
		qn.mainHwnd, 0, hInst, 0,
	)
	qnSendMessageW.Call(qn.editHwnd, qnWmSetFont, qn.editFont, 1)

	placeholder, _ := windows.UTF16PtrFromString("  Type a thought, idea or frustration... (Enter saves, Esc dismisses)")
	qnSendMessageW.Call(qn.editHwnd, qnEmSetCueBanner, 0, uintptr(unsafe.Pointer(placeholder)))

	qnRegisterHotKey.Call(0, qnHotKeyID, qnModCtrl|qnModShift, 0x20)
	log.Println("[QuickNote] Overlay ready. Hotkey: Ctrl+Shift+Space")

	threadID, _, _ := qnGetCurrentThreadId.Call()

	var lastCPress time.Time
	hookCB := syscall.NewCallback(func(nCode int, wParam uintptr, lParam uintptr) uintptr {
		if nCode >= 0 {
			if wParam == qnWmKeydown || wParam == 0x0104 { // 0x0104 = WM_SYSKEYDOWN
				kbdstruct := (*qnKBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
				if kbdstruct.VkCode == 0x43 { // 'C'
					ctrlState, _, _ := qnGetAsyncKeyState.Call(0x11) // VK_CONTROL
					if (ctrlState & 0x8000) != 0 {
						now := time.Now()
						if now.Sub(lastCPress) < 700*time.Millisecond {
							qnPostThreadMessageW.Call(threadID, qnWmHotkey, qnHotKeyID, 0)
							lastCPress = time.Time{} // reset
						} else {
							lastCPress = now
						}
					}
				} else if kbdstruct.VkCode != 0x11 && kbdstruct.VkCode != 0xA2 && kbdstruct.VkCode != 0xA3 {
					// Reset if another key (not Ctrl and not C) is pressed
					lastCPress = time.Time{}
				}
			}
		}
		ret, _, _ := qnCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	})

	hookHandle, _, _ := qnSetWindowsHookExW.Call(13, hookCB, 0, 0) // WH_KEYBOARD_LL = 13

	go func() {
		<-ctx.Done()
		if hookHandle != 0 {
			qnUnhookWindowsHookEx.Call(hookHandle)
		}
		qnUnregisterHotKey.Call(0, qnHotKeyID)
		qnDeleteObject.Call(qn.bgBrush)
		qnDeleteObject.Call(qn.editFont)
		qnDeleteObject.Call(qn.btnFont)
		qnPostThreadMessageW.Call(threadID, qnWmQuit, 0, 0)
	}()

	var msg qnMSG
	for {
		ret, _, _ := qnGetMessageW2.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}

		if uintptr(msg.Message) == qnWmHotkey && msg.WParam == qnHotKeyID {
			if qn.visible {
				qn.hide()
			} else {
				// Reset context on show
				qn.activeBtn = 0
				qnInvalidateRect.Call(qn.mainHwnd, 0, 1)
				qn.show()
			}
			continue
		}

		if uintptr(msg.Message) == qnWmKeydown && msg.Hwnd == qn.editHwnd {
			switch msg.WParam {
			case qnVkReturn:
				qn.saveAndHide()
				continue
			case qnVkEscape:
				qn.hide()
				continue
			}
		}

		qnTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		qnDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (qn *QuickNoteWatcher) show() {
	tags := qn.getTags()
	qn.tagBtns = []qnButton{}
	qn.selectedTags = make(map[int]bool)
	
	startX := int32(14)
	for i, t := range tags {
		wStr, _ := windows.UTF16PtrFromString(t)
		btnW := int32(16 + len(t)*8) // roughly 8px per char
		qn.tagBtns = append(qn.tagBtns, qnButton{
			ID:    i,
			Label: wStr,
			Rect:  qnRECT{startX, 94, startX + btnW, 118},
			Type:  t,
		})
		startX += btnW + 8
	}

	qnShowWindow.Call(qn.mainHwnd, qnSwShow)
	qnSetFocus.Call(qn.editHwnd)
	qn.visible = true
}

func (qn *QuickNoteWatcher) hide() {
	qnShowWindow.Call(qn.mainHwnd, qnSwHide)
	qn.visible = false
}

func (qn *QuickNoteWatcher) saveAndHide() {
	buf := make([]uint16, 512)
	qnGetWindowTextW.Call(qn.editHwnd, uintptr(unsafe.Pointer(&buf[0])), 512)
	text := windows.UTF16ToString(buf)

	if len([]rune(text)) > 2 {
		appendedTags := ""
		for _, b := range qn.tagBtns {
			if qn.selectedTags[b.ID] {
				appendedTags += " " + b.Type
			}
		}
		if appendedTags != "" {
			text += appendedTags
		}

		ctxType := "standard"
		if qn.activeBtn >= 0 && qn.activeBtn < len(qn.contextBtns) {
			ctxType = qn.contextBtns[qn.activeBtn].Type
		}

		var clipText string
		var filePaths []string

		if ctxType == "clipboard" || ctxType == "file" {
			openRet, _, _ := qnOpenClipboard.Call(0)
			if openRet != 0 {
				if ctxType == "clipboard" {
					ok, _, _ := qnIsClipboardFormatAvailable.Call(qnCfUnicodeText)
					if ok != 0 {
						hData, _, _ := qnGetClipboardData.Call(qnCfUnicodeText)
						if hData != 0 {
							ptr, _, _ := qnGlobalLock.Call(hData)
							if ptr != 0 {
								rawText := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr)))
								qnGlobalUnlock.Call(hData)
								if qn.redactor != nil {
									clipText = qn.redactor(rawText)
								} else {
									clipText = rawText
								}
							}
						}
					}
				} else if ctxType == "file" {
					ok, _, _ := qnIsClipboardFormatAvailable.Call(qnCfHDrop)
					if ok != 0 {
						hDrop, _, _ := qnGetClipboardData.Call(qnCfHDrop)
						if hDrop != 0 {
							count, _, _ := qnDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
							for i := uintptr(0); i < count; i++ {
								pathBuf := make([]uint16, 260)
								qnDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&pathBuf[0])), 260)
								path := windows.UTF16ToString(pathBuf)
								if path != "" {
									filePaths = append(filePaths, path)
								}
							}
						}
					}
				}
				qnCloseClipboard.Call()
			}
		}

		payload, _ := json.Marshal(QuickNotePayload{
			Note:          text,
			ContextType:   ctxType,
			ClipboardText: clipText,
			FilePaths:     filePaths,
		})
		qn.out <- &Event{
			EventID: "evt_note_" + time.Now().Format("20060102150405.000"),
			TsStart: time.Now(),
			TsEnd:   time.Now(),
			Type:    EventQuickNote,
			Context: EventContext{OSPlatform: "windows"},
			Payload: payload,
		}
		log.Printf("[QuickNote] Saved %s note (%d chars)", ctxType, len(text))
	}

	emptyStr, _ := windows.UTF16PtrFromString("")
	qnSetWindowTextW.Call(qn.editHwnd, uintptr(unsafe.Pointer(emptyStr)))
	qn.hide()
}

func qnWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case qnWmCtlColorEdit:
		qnSetTextColor.Call(wParam, qnColorText)
		qnSetBkColor.Call(wParam, qnColorBg)
		if qnActive != nil {
			return qnActive.bgBrush
		}

	case qnWmLButtonDown:
		if qnActive != nil && hwnd == qnActive.mainHwnd {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			for _, b := range qnActive.contextBtns {
				if x >= b.Rect.Left && x <= b.Rect.Right && y >= b.Rect.Top && y <= b.Rect.Bottom {
					if qnActive.activeBtn != b.ID {
						qnActive.activeBtn = b.ID
						qnInvalidateRect.Call(hwnd, 0, 1) // Trigger WM_PAINT to redraw buttons
					}
					return 0
				}
			}
			for _, b := range qnActive.tagBtns {
				if x >= b.Rect.Left && x <= b.Rect.Right && y >= b.Rect.Top && y <= b.Rect.Bottom {
					qnActive.selectedTags[b.ID] = !qnActive.selectedTags[b.ID]
					qnInvalidateRect.Call(hwnd, 0, 1)
					return 0
				}
			}
			return 0
		}

	case qnWmPaint:
		if qnActive != nil && hwnd == qnActive.mainHwnd {
			var ps qnPAINTSTRUCT
			hdc, _, _ := qnBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

			qnSetBkMode.Call(hdc, 1) // TRANSPARENT
			qnSelectObject.Call(hdc, qnActive.btnFont)

			brushActive, _, _ := qnCreateSolidBrush.Call(qnColorBtnActive)
			brushInactive, _, _ := qnCreateSolidBrush.Call(qnColorBtnInactive)
			brushTagBg, _, _ := qnCreateSolidBrush.Call(0x00332222) // dark subtle tag bg
			penNull, _, _ := qnCreatePen.Call(qnPsNull, 0, 0)
			
			// Remove border drawing
			qnSelectObject.Call(hdc, penNull)

			for _, b := range qnActive.contextBtns {
				if b.ID == qnActive.activeBtn {
					qnSelectObject.Call(hdc, brushActive)
					qnSetTextColor.Call(hdc, qnColorText)
				} else {
					qnSelectObject.Call(hdc, brushInactive)
					qnSetTextColor.Call(hdc, qnColorTextMuted)
				}
				
				// Draw rounded rectangle pill
				qnRoundRect.Call(hdc, uintptr(b.Rect.Left), uintptr(b.Rect.Top), uintptr(b.Rect.Right), uintptr(b.Rect.Bottom), 10, 10)
				
				// Draw centered text
				qnDrawTextW.Call(hdc, uintptr(unsafe.Pointer(b.Label)), ^uintptr(0), uintptr(unsafe.Pointer(&b.Rect)), qnDtCenter|qnDtVCenter|qnDtSingleLine)
			}

			for _, b := range qnActive.tagBtns {
				if qnActive.selectedTags[b.ID] {
					qnSelectObject.Call(hdc, brushActive)
					qnSetTextColor.Call(hdc, qnColorText)
				} else {
					qnSelectObject.Call(hdc, brushTagBg)
					qnSetTextColor.Call(hdc, qnColorTextMuted)
				}
				qnRoundRect.Call(hdc, uintptr(b.Rect.Left), uintptr(b.Rect.Top), uintptr(b.Rect.Right), uintptr(b.Rect.Bottom), 8, 8)
				qnDrawTextW.Call(hdc, uintptr(unsafe.Pointer(b.Label)), ^uintptr(0), uintptr(unsafe.Pointer(&b.Rect)), qnDtCenter|qnDtVCenter|qnDtSingleLine)
			}

			qnDeleteObject.Call(brushActive)
			qnDeleteObject.Call(brushInactive)
			qnDeleteObject.Call(brushTagBg)
			qnDeleteObject.Call(penNull)

			qnEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			return 0
		}

	case qnWmDestroy:
		qnPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := qnDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}
