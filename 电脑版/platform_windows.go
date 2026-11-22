//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForeground       = user32.NewProc("SetForegroundWindow")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procIsIconic            = user32.NewProc("IsIconic")
	procGetWindowPlacement  = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement  = user32.NewProc("SetWindowPlacement")
	procMonitorFromRect     = user32.NewProc("MonitorFromRect")
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procAllowSetForeground  = user32.NewProc("AllowSetForegroundWindow")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procFlashWindow         = user32.NewProc("FlashWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	dwmapi                  = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttr    = dwmapi.NewProc("DwmSetWindowAttribute")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }
type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    point
	PtMaxPosition    point
	RcNormalPosition rect
}

type savedPlacement struct {
	L, T, R, B int32
	Max        bool
}

type uiHandle struct{ w webview2.WebView }

func (u *uiHandle) focus() {
	if u.w == nil {
		return
	}
	w := u.w
	w.Dispatch(func() {
		h := uintptr(w.Window())
		if r, _, _ := procIsIconic.Call(h); r != 0 {
			procShowWindow.Call(h, 9) // SW_RESTORE
		}
		procBringWindowToTop.Call(h)
		if r, _, _ := procSetForeground.Call(h); r == 0 {
			procFlashWindow.Call(h, 1)
		}
	})
}

// singleInstance returns false when another 拾音 is already running (and asks it to come to front).
func singleInstance() bool {
	name, _ := windows.UTF16PtrFromString("Local\\ShiyinPC.SingleInstance")
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h != 0 && err != windows.ERROR_ALREADY_EXISTS {
		return true // we own it; the handle lives until the process exits
	}
	procAllowSetForeground.Call(0xFFFFFFFF) // ASFW_ANY: let the running copy take focus
	if b, err := os.ReadFile(filepath.Join(dataDir, "port.txt")); err == nil {
		if port, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			c := &http.Client{Timeout: 2 * time.Second}
			if resp, err := c.Post("http://127.0.0.1:"+strconv.Itoa(port)+"/api/focus", "application/json", nil); err == nil {
				resp.Body.Close()
			}
		}
	}
	return false
}

func systemDPI() uint {
	if procGetDpiForSystem.Find() != nil {
		return 96
	}
	d, _, _ := procGetDpiForSystem.Call()
	if d < 96 {
		return 96
	}
	return uint(d)
}

func runUI(u *uiHandle, url string) {
	dpi := systemDPI()
	// 0.2.7: paint the window in the theme's colour from the start (no white box while WebView2 loads)
	dark := getConfig().Theme != "light" && !(getConfig().Theme == "system" && systemLight())
	bg := "FF0D0D0F"
	if !dark {
		bg = "FFFFFFFF"
	}
	oldBG, hadBG := os.LookupEnv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR")
	os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", bg)
	wo := webview2.WindowOptions{Title: "拾音", Width: 1200 * dpi / 96, Height: 780 * dpi / 96, IconId: 1, Center: true}
	// 0.2.8: open straight at last time's place/size (maximized if it was), no small-then-big jump
	if sp, ok := savedWindow(); ok {
		wo.UsePos, wo.X, wo.Y = true, sp.L, sp.T
		wo.Width, wo.Height, wo.Maximized = uint(sp.R-sp.L), uint(sp.B-sp.T), sp.Max
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:         os.Getenv("SHIYIN_DEBUG") != "",
		AutoFocus:     true,
		DataPath:      filepath.Join(dataDir, "webview2"),
		WindowOptions: wo,
	})
	if hadBG {
		os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", oldBG)
	} else {
		os.Unsetenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR")
	}
	if w == nil {
		openBrowser(url)
		msgBox("拾音", "没有找到 Microsoft Edge WebView2 运行库，已改用默认浏览器打开拾音。\n关闭此对话框将退出拾音。\n\n安装 WebView2 后即可使用独立窗口：\nhttps://go.microsoft.com/fwlink/p/?LinkId=2124703")
		return
	}
	u.w = w
	defer w.Destroy()
	applyTitleBar(uintptr(w.Window()), dark, false)
	w.SetSize(int(960*dpi/96), int(600*dpi/96), webview2.HintMin)
	hwnd := uintptr(w.Window())
	restorePlacement(hwnd)
	stop := make(chan struct{})
	go trackPlacement(hwnd, stop)
	w.Navigate(url)
	u.deskStartup() // 0.2.4 桌面歌词
	w.Run()
	close(stop)
}

func placementPath() string { return filepath.Join(dataDir, "window.json") }

// savedWindow returns last time's window placement if it is sane and its monitor still exists.
func savedWindow() (savedPlacement, bool) {
	var sp savedPlacement
	if !loadJSON("window.json", &sp) || sp.R-sp.L < 400 || sp.B-sp.T < 300 {
		return sp, false
	}
	r := rect{sp.L, sp.T, sp.R, sp.B}
	if m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 0); m == 0 {
		return sp, false
	}
	return sp, true
}

func restorePlacement(hwnd uintptr) {
	var sp savedPlacement
	if !loadJSON("window.json", &sp) || sp.R-sp.L < 400 || sp.B-sp.T < 300 {
		return
	}
	r := rect{sp.L, sp.T, sp.R, sp.B}
	if m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 0); m == 0 {
		return // that monitor is gone: keep the centered default
	}
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
	wp.RcNormalPosition = r
	wp.ShowCmd = 1 // SW_SHOWNORMAL
	if sp.Max {
		wp.ShowCmd = 3 // SW_SHOWMAXIMIZED
	}
	procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
}

func readPlacement(hwnd uintptr) (savedPlacement, bool) {
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return savedPlacement{}, false
	}
	if wp.ShowCmd == 2 || wp.ShowCmd == 6 || wp.ShowCmd == 7 { // minimized: keep the normal rect only
		wp.ShowCmd = 1
	}
	n := wp.RcNormalPosition
	return savedPlacement{n.Left, n.Top, n.Right, n.Bottom, wp.ShowCmd == 3}, true
}

// trackPlacement samples the window placement and saves it when it changes,
// so size/position/maximized survive however the window is closed.
func trackPlacement(hwnd uintptr, stop chan struct{}) {
	var last savedPlacement
	if b, err := os.ReadFile(placementPath()); err == nil {
		json.Unmarshal(b, &last)
	}
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if p, ok := readPlacement(hwnd); ok && p != last && p.R-p.L >= 400 {
				last = p
				saveJSON("window.json", p)
			}
		}
	}
}

func openBrowser(url string) {
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func revealFile(path string) {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + path + `"`}
	cmd.Start()
}

func openFolder(path string) {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer "` + path + `"`}
	cmd.Start()
}

func msgBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40)
}

func fatalBox(text string) { msgBox("拾音", text) }

// ---------- dark / light title bar ----------

// applyTitleBar sets DWMWA_USE_IMMERSIVE_DARK_MODE (20; 19 on Windows 10 before 20H1) and repaints the caption.
func applyTitleBar(hwnd uintptr, dark bool, repaint bool) {
	if hwnd == 0 || procDwmSetWindowAttr.Find() != nil {
		return
	}
	var v int32
	if dark {
		v = 1
	}
	if r, _, _ := procDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4); r != 0 {
		procDwmSetWindowAttr.Call(hwnd, 19, uintptr(unsafe.Pointer(&v)), 4)
	}
	if !repaint {
		return
	}
	// SWP_NOSIZE|SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0001|0x0002|0x0004|0x0010|0x0020)
	// Windows 10 only repaints the caption on (de)activation: toggle WM_NCACTIVATE
	fg, _, _ := procGetForegroundWindow.Call()
	procSendMessageW.Call(hwnd, 0x0086, 0, 0)
	if fg == hwnd {
		procSendMessageW.Call(hwnd, 0x0086, 1, 0)
	}
}

func (u *uiHandle) setDark(dark bool) {
	if u == nil || u.w == nil {
		return
	}
	w := u.w
	w.Dispatch(func() { applyTitleBar(uintptr(w.Window()), dark, true) })
}

// systemLight reports whether Windows apps use the light theme (AppsUseLightTheme).
func systemLight() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 1
}

func (u *uiHandle) quit() {
	go func() { time.Sleep(3 * time.Second); os.Exit(0) }()
	if u == nil || u.w == nil {
		os.Exit(0)
	}
	w := u.w
	w.Dispatch(func() { w.Terminate() })
}

// ---------- system proxy (Internet Options; what Clash / v2rayN set) ----------

var (
	proxyMu   sync.Mutex
	proxyAt   time.Time
	proxyVals map[string]string
)

func systemProxy(scheme string) string {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if time.Since(proxyAt) > 20*time.Second {
		proxyAt = time.Now()
		proxyVals = map[string]string{}
		k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
		if err == nil {
			en, _, _ := k.GetIntegerValue("ProxyEnable")
			srv, _, _ := k.GetStringValue("ProxyServer")
			k.Close()
			if en == 1 && strings.TrimSpace(srv) != "" {
				if !strings.Contains(srv, "=") {
					proxyVals["http"], proxyVals["https"] = srv, srv
				} else {
					for _, part := range strings.Split(srv, ";") {
						kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
						if len(kv) == 2 {
							proxyVals[strings.ToLower(kv[0])] = kv[1]
						}
					}
					if proxyVals["https"] == "" && proxyVals["http"] != "" {
						proxyVals["https"] = proxyVals["http"]
					}
					if s := proxyVals["socks"]; s != "" && proxyVals["https"] == "" {
						proxyVals["https"], proxyVals["http"] = "socks5://"+s, "socks5://"+s
					}
				}
			}
		}
	}
	return proxyVals[scheme]
}

// runInstaller starts the NSIS installer silently; it closes 拾音, upgrades and relaunches it.
func runInstaller(path string) error {
	cmd := exec.Command(path, "/S")
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	return cmd.Start()
}
