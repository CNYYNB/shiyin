//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

type uiHandle struct{}

func (u *uiHandle) focus() {}

func (u *uiHandle) setDark(dark bool) { fmt.Println("titlebar dark", dark) }

func (u *uiHandle) quit() { fmt.Println("quit"); os.Exit(0) }

func systemProxy(scheme string) string { return "" }

// runInstaller (non-Windows, tests only): runs $SHIYIN_TEST_INSTALL_CMD <installer>.
func runInstaller(path string) error {
	c := os.Getenv("SHIYIN_TEST_INSTALL_CMD")
	if c == "" {
		return errors.New("只能在 Windows 上安装")
	}
	return exec.Command("sh", "-c", c+" \"$0\"", path).Start()
}

func singleInstance() bool { return true }

func runUI(u *uiHandle, url string) {
	fmt.Println("SHIYIN_URL " + url)
	if os.Getenv("SHIYIN_HEADLESS") == "" {
		exec.Command("xdg-open", url).Start()
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func revealFile(path string) { fmt.Println("reveal", path) }
func openFolder(path string) { fmt.Println("open", path) }
func fatalBox(text string)   { fmt.Fprintln(os.Stderr, text) }

// deskApply (non-Windows, tests only): the 桌面歌词 window exists only on Windows.
func (u *uiHandle) deskApply(p DeskPrefs) {
	fmt.Printf("desk apply on=%v locked=%v size=%d color=%s\n", p.On, p.Locked, p.Size, p.Color)
}

// non-Windows (tests): pretend the global hotkey registered; SHIYIN_DESK_HOTKEY=0 simulates "taken".
func init() {
	if os.Getenv("SHIYIN_DESK_HOTKEY") != "0" {
		deskHotkey = "Ctrl+Alt+L"
	}
}
