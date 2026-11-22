//go:build windows

package main

// 0.2.4 桌面歌词 window: WS_POPUP tool window (no taskbar button, not in Alt-Tab), topmost, WS_EX_NOACTIVATE
// (never takes focus) hosting its own WebView2 controller with a transparent DefaultBackgroundColor.
// Per-pixel transparency uses the DwmEnableBlurBehindWindow + empty-region trick with a black class brush
// (same as tao/Tauri). 锁定 adds WS_EX_LAYERED|WS_EX_TRANSPARENT → clicks go through to the desktop.
// Everything here runs on the UI thread (webview Dispatch / window procedure / WebView2 callbacks).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

var (
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	procSetLayeredWinAttr   = user32.NewProc("SetLayeredWindowAttributes")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procReleaseCapture      = user32.NewProc("ReleaseCapture")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procFillRect            = user32.NewProc("FillRect")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procRegisterHotKey      = user32.NewProc("RegisterHotKey")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	gdi32                   = windows.NewLazySystemDLL("gdi32.dll")
	procGetStockObject      = gdi32.NewProc("GetStockObject")
	procCreateRectRgn       = gdi32.NewProc("CreateRectRgn")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDwmEnableBlurBehind = dwmapi.NewProc("DwmEnableBlurBehindWindow")
	deskClassName, _        = windows.UTF16PtrFromString("ShiyinDeskLyrics")
	deskTitle, _            = windows.UTF16PtrFromString("拾音 桌面歌词")
	dw                      *deskWin
	deskUI                  *uiHandle
	deskClassOK             bool
	deskHotHwnd             uintptr // message-only window owning the Ctrl+Alt+L hotkey
)

const (
	wsPopup, wsClipChildren     = 0x80000000, 0x02000000
	wsExToolWindow, wsExTopmost = 0x00000080, 0x00000008
	wsExNoActivate, wsExLayered = 0x08000000, 0x00080000
	wsExTransparent             = 0x00000020
	gwlExStyle                  = ^uintptr(19) // -20
	hwndTopmost                 = ^uintptr(0)  // -1
)

type deskWin struct {
	hwnd   uintptr
	ch     *edge.Chromium
	shown  bool
	locked bool
	// 0.2.5 unlock pill: cursor polled at 10 Hz only while locked + shown
	polling  bool
	overWin  bool
	overPill bool     // WS_EX_TRANSPARENT removed while the cursor is on the pill
	pill     [4]int32 // CSS px, reported by desk.html
	pillOK   bool
	// width resize from the edge grips
	sizing   bool
	sizeLeft bool
	sizeX0   int32
	sizeR0   rect
}

const deskTimerID, deskSizeTimerID = 1, 2

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type dwmBlurBehind struct {
	Flags                 uint32
	Enable                int32
	RgnBlur               uintptr
	TransitionOnMaximized int32
}

func (u *uiHandle) deskApply(p DeskPrefs) {
	if u == nil || u.w == nil {
		return
	}
	u.w.Dispatch(func() { deskApplyUI(u, p) })
}

// deskStartup is called once the main window exists (UI thread).
func (u *uiHandle) deskStartup() {
	deskUI = u
	deskHotkeyInit()
	if p := getDeskPrefs(); p.On {
		u.deskApply(p)
	}
}

func dpiScale(v int) int32 { return int32(v * int(systemDPI()) / 96) }

func deskApplyUI(u *uiHandle, p DeskPrefs) {
	if !p.On {
		if dw != nil && dw.shown {
			procShowWindow.Call(dw.hwnd, 0) // SW_HIDE
			dw.ch.Hide()
			dw.shown = false
			deskPoll()
		}
		return
	}
	created := false
	if dw == nil {
		if !deskCreate(u) {
			return
		}
		created = true
	}
	h := dpiScale(deskHeightCSS(p.Size))
	var x, y, w int32
	if dw.shown {
		var r rect
		procGetWindowRect.Call(dw.hwnd, uintptr(unsafe.Pointer(&r)))
		x, y, w = r.Left, r.Top, r.Right-r.Left
	} else if p.Pos {
		x, y, w = p.X, p.Y, p.W
	} else { // default: bottom centre of the primary monitor's work area
		wa := workArea(rect{0, 0, 1, 1})
		w = dpiScale(deskWidthDef)
		x, y = (wa.Left+wa.Right-w)/2, wa.Bottom-h-dpiScale(64)
	}
	wa := workArea(rect{x, y, x + w, y + h})
	x, y, w = clampDeskRect(x, y, w, h, wa.Left, wa.Top, wa.Right, wa.Bottom)
	procSetWindowPos.Call(dw.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0010|0x0040) // NOACTIVATE|SHOWWINDOW
	if !dw.shown {
		dw.ch.Show()
		dw.shown = true
	}
	if created || dw.locked != p.Locked {
		deskSetLock(p.Locked)
	}
	deskPoll()
}

// workArea of the monitor nearest to r (per-monitor restore; a removed monitor falls back to the nearest one).
func workArea(r rect) rect {
	m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 2) // MONITOR_DEFAULTTONEAREST
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if m == 0 {
		return rect{0, 0, 1280, 720}
	}
	procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi.Work
}

func deskRegisterClass() uintptr {
	var inst windows.Handle
	windows.GetModuleHandleEx(0, nil, &inst)
	if !deskClassOK {
		black, _, _ := procGetStockObject.Call(4) // BLACK_BRUSH: black + blur-behind = transparent
		cur, _, _ := procLoadCursorW.Call(0, 32512)
		wc := wndClassEx{WndProc: windows.NewCallback(deskWndProc), Instance: uintptr(inst), Cursor: cur, Background: black, ClassName: deskClassName}
		wc.Size = uint32(unsafe.Sizeof(wc))
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		deskClassOK = true
	}
	return uintptr(inst)
}

// deskHotkeyInit: Ctrl+Alt+L (global) toggles lock. Owned by a hidden message-only window of the desk class so it
// works before the desk window exists. A failed registration (taken by another app) is silently skipped.
func deskHotkeyInit() {
	if deskHotHwnd != 0 {
		return
	}
	inst := deskRegisterClass()
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(deskClassName)), 0, 0, 0, 0, 0, 0,
		^uintptr(2), 0, inst, 0) // parent HWND_MESSAGE (-3)
	if h == 0 {
		return
	}
	deskHotHwnd = h
	ok, _, _ := procRegisterHotKey.Call(h, 1, 0x0001|0x0002|0x4000, 'L') // MOD_ALT|MOD_CONTROL|MOD_NOREPEAT
	if ok != 0 {
		deskHotkey = "Ctrl+Alt+L"
	}
}

func deskCreate(u *uiHandle) bool {
	inst := deskRegisterClass()
	hwnd, _, _ := procCreateWindowExW.Call(wsExToolWindow|wsExTopmost|wsExNoActivate,
		uintptr(unsafe.Pointer(deskClassName)), uintptr(unsafe.Pointer(deskTitle)),
		wsPopup|wsClipChildren, 0, 0, 100, 100, 0, 0, inst, 0)
	if hwnd == 0 {
		return false
	}
	deskTransparent(hwnd)
	if procDwmSetWindowAttr.Find() == nil {
		v := int32(1) // DWMWA_WINDOW_CORNER_PREFERENCE = DONOTROUND (Windows 11)
		procDwmSetWindowAttr.Call(hwnd, 33, uintptr(unsafe.Pointer(&v)), 4)
		c := uint32(0xFFFFFFFE) // DWMWA_BORDER_COLOR = none
		procDwmSetWindowAttr.Call(hwnd, 34, uintptr(unsafe.Pointer(&c)), 4)
		nc := int32(1) // DWMWA_NCRENDERING_POLICY = DWMNCRP_DISABLED: DWM never draws a frame here
		procDwmSetWindowAttr.Call(hwnd, 2, uintptr(unsafe.Pointer(&nc)), 4)
		bd := int32(1) // DWMWA_SYSTEMBACKDROP_TYPE = DWMSBT_NONE (Win11 22H2+: no Mica/Acrylic)
		procDwmSetWindowAttr.Call(hwnd, 38, uintptr(unsafe.Pointer(&bd)), 4)
	}
	ch := edge.NewChromium()
	ch.DataPath = filepath.Join(dataDir, "webview2")
	ch.MessageCallback = deskMsg
	dw = &deskWin{hwnd: hwnd, ch: ch}
	// transparent from the very first frame (before put_DefaultBackgroundColor below); read when the controller is
	// created, so only this window's controller gets it (the main window's already exists).
	old, had := os.LookupEnv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR")
	os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "00000000")
	ok := ch.Embed(hwnd)
	if had {
		os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", old)
	} else {
		os.Unsetenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR")
	}
	if !ok {
		dw = nil
		procPostMessageW.Call(hwnd, 0x0010, 0, 0)
		return false
	}
	if s, err := ch.GetSettings(); err == nil {
		s.PutAreDefaultContextMenusEnabled(false)
		s.PutAreDevToolsEnabled(os.Getenv("SHIYIN_DEBUG") != "")
		s.PutIsZoomControlEnabled(false)
		s.PutIsStatusBarEnabled(false)
		s.PutAreBrowserAcceleratorKeysEnabled(false)
	}
	if c2 := ch.GetController().GetICoreWebView2Controller2(); c2 != nil {
		c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{}) // A=0 → transparent
	}
	ch.Resize()
	ch.Navigate(deskURL)
	if u != nil && u.w != nil {
		u.w.Dispatch(func() {}) // Embed's own message loop swallowed any pending WM_APP: re-post one
	}
	return true
}

func deskTransparent(hwnd uintptr) {
	if procDwmEnableBlurBehind.Find() != nil {
		return
	}
	rgn, _, _ := procCreateRectRgn.Call(0, 0, ^uintptr(0), ^uintptr(0)) // (0,0,-1,-1): empty, outside the window
	bb := dwmBlurBehind{Flags: 1 | 2, Enable: 1, RgnBlur: rgn}          // DWM_BB_ENABLE | DWM_BB_BLURREGION
	procDwmEnableBlurBehind.Call(hwnd, uintptr(unsafe.Pointer(&bb)))
	procDeleteObject.Call(rgn)
}

func deskSetLock(locked bool) {
	ex, _, _ := procGetWindowLongPtrW.Call(dw.hwnd, gwlExStyle)
	if locked {
		ex |= wsExLayered | wsExTransparent
	} else {
		ex &^= wsExLayered | wsExTransparent
	}
	procSetWindowLongPtrW.Call(dw.hwnd, gwlExStyle, ex)
	if locked {
		procSetLayeredWinAttr.Call(dw.hwnd, 0, 255, 2) // LWA_ALPHA 255: layered but fully drawn
	}
	deskTransparent(dw.hwnd)                                                             // style changes can reset the DWM blur-behind state
	procSetWindowPos.Call(dw.hwnd, hwndTopmost, 0, 0, 0, 0, 0x0001|0x0002|0x0010|0x0020) // NOSIZE|NOMOVE|NOACTIVATE|FRAMECHANGED
	dw.locked = locked
	dw.overWin, dw.overPill = false, false
	deskEval("window.deskLockHover && deskLockHover(false)")
}

// deskPoll starts/stops the 10 Hz cursor timer: only while locked and visible.
func deskPoll() {
	if dw == nil {
		return
	}
	want := dw.locked && dw.shown
	if want && !dw.polling {
		procSetTimer.Call(dw.hwnd, deskTimerID, 100, 0)
	} else if !want && dw.polling {
		procKillTimer.Call(dw.hwnd, deskTimerID)
		if dw.overPill {
			deskClickThrough(true)
		}
		if dw.overWin {
			deskEval("window.deskLockHover && deskLockHover(false)")
		}
		dw.overWin, dw.overPill = false, false
	}
	dw.polling = want
}

func deskEval(js string) {
	if dw != nil && dw.ch != nil {
		dw.ch.Eval(js)
	}
}

// deskClickThrough toggles only WS_EX_TRANSPARENT (WS_EX_LAYERED stays) while locked: off while the cursor is on
// the unlock pill so it can be clicked; the DWM blur-behind is re-applied after every style change (Win10 flash).
func deskClickThrough(through bool) {
	ex, _, _ := procGetWindowLongPtrW.Call(dw.hwnd, gwlExStyle)
	if through {
		ex |= wsExTransparent
	} else {
		ex &^= wsExTransparent
	}
	procSetWindowLongPtrW.Call(dw.hwnd, gwlExStyle, ex)
	deskTransparent(dw.hwnd)
	dw.overPill = !through
}

func deskLockTick() {
	if dw == nil || !dw.locked || !dw.shown {
		return
	}
	var c point
	var r rect
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&c))); ok == 0 {
		return
	}
	procGetWindowRect.Call(dw.hwnd, uintptr(unsafe.Pointer(&r)))
	dpi := int32(systemDPI())
	pill := dw.pill
	if !dw.pillOK {
		pill = deskPillDef((r.Right - r.Left) * 96 / dpi)
	}
	overWin, overPill := deskLockHit(c.X, c.Y, r.Left, r.Top, r.Right, r.Bottom, pill, dpi)
	if overWin != dw.overWin {
		dw.overWin = overWin
		deskEval(fmt.Sprintf("window.deskLockHover && deskLockHover(%v)", overWin))
	}
	if overPill != dw.overPill {
		deskClickThrough(!overPill)
	}
}

// deskMsg: window.chrome.webview.postMessage from desk.html (UI thread).
func deskMsg(m string) {
	if dw == nil {
		return
	}
	if strings.HasPrefix(m, "pill:") { // unlock pill rect in CSS px: "pill:l,t,r,b"
		var v [4]int32
		if n, _ := fmt.Sscanf(m[5:], "%d,%d,%d,%d", &v[0], &v[1], &v[2], &v[3]); n == 4 && v[2] > v[0] && v[3] > v[1] {
			dw.pill, dw.pillOK = v, true
		}
		return
	}
	if dw.locked {
		return
	}
	switch m {
	case "drag":
		procReleaseCapture.Call()
		procPostMessageW.Call(dw.hwnd, 0x00A1, 2, 0) // WM_NCLBUTTONDOWN HTCAPTION → system move loop
	case "size-l", "size-r":
		// 0.2.5: no WS_THICKFRAME any more (its sizing border was painted as an opaque white frame around the
		// transparent window) → own width-resize loop: follow the cursor at ~60 Hz until the left button is up.
		var c point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&c)))
		var r rect
		procGetWindowRect.Call(dw.hwnd, uintptr(unsafe.Pointer(&r)))
		dw.sizing, dw.sizeLeft, dw.sizeX0, dw.sizeR0 = true, m == "size-l", c.X, r
		procSetTimer.Call(dw.hwnd, deskSizeTimerID, 16, 0)
	}
}

func deskSizeTick() {
	var c point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&c)))
	down, _, _ := procGetAsyncKeyState.Call(0x01) // VK_LBUTTON
	r0 := dw.sizeR0
	wa := workArea(r0)
	x, w := deskResizeX(dw.sizeLeft, r0.Left, r0.Right, c.X-dw.sizeX0, dpiScale(deskWidthMin), wa.Left, wa.Right)
	procSetWindowPos.Call(dw.hwnd, 0, uintptr(x), uintptr(r0.Top), uintptr(w), uintptr(r0.Bottom-r0.Top), 0x0004|0x0010) // NOZORDER|NOACTIVATE
	if down&0x8000 == 0 {
		procKillTimer.Call(dw.hwnd, deskSizeTimerID)
		dw.sizing = false
		deskSavePos()
	}
}

func deskSavePos() {
	if dw == nil {
		return
	}
	var r rect
	procGetWindowRect.Call(dw.hwnd, uintptr(unsafe.Pointer(&r)))
	updateDesk(func(p *DeskPrefs) { p.Pos, p.X, p.Y, p.W = true, r.Left, r.Top, r.Right-r.Left }, false)
}

func deskWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case 0x0083: // WM_NCCALCSIZE: no frame at all
		if wp != 0 {
			return 0
		}
	case 0x0021: // WM_MOUSEACTIVATE
		return 3 // MA_NOACTIVATE
	case 0x0014: // WM_ERASEBKGND: black = transparent
		var r rect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		b, _, _ := procGetStockObject.Call(4)
		procFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), b)
		return 1
	case 0x0005: // WM_SIZE
		if dw != nil && dw.hwnd == hwnd {
			dw.ch.Resize()
		}
	case 0x0003, 0x0216: // WM_MOVE, WM_MOVING
		if dw != nil && dw.hwnd == hwnd {
			dw.ch.NotifyParentWindowPositionChanged()
		}
	case 0x0113: // WM_TIMER: locked → cursor over the lyrics? (unlock pill)
		if dw != nil && dw.hwnd == hwnd {
			if wp == deskTimerID {
				deskLockTick()
			} else if wp == deskSizeTimerID && dw.sizing {
				deskSizeTick()
			}
			return 0
		}
	case 0x0312: // WM_HOTKEY Ctrl+Alt+L
		if hwnd == deskHotHwnd {
			go deskToggleLock()
			return 0
		}
	case 0x0232: // WM_EXITSIZEMOVE
		deskSavePos()
	case 0x007E: // WM_DISPLAYCHANGE: keep it on a visible screen
		if dw != nil && dw.shown && deskUI != nil {
			deskUI.deskApply(getDeskPrefs())
		}
	case 0x0010: // WM_CLOSE (Alt+F4 etc.): just turn 桌面歌词 off
		if dw != nil && dw.hwnd == hwnd {
			go updateDesk(func(p *DeskPrefs) { p.On = false }, true)
			return 0
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}
