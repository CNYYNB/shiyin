package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const appVersion = "0.3.0"

// deskURL opens the 桌面歌词 page (desklyrics_windows.go).
var deskURL string

func main() {
	initDataDir()
	if f, err := os.OpenFile(filepath.Join(dataDir, "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(f)
	}
	if !singleInstance() {
		return // already running: the other window was brought to front
	}
	loadConfig()
	loadLibrary()
	loadMeta()
	loadPlaylists()
	loadPrefs()
	cleanupUpdateFiles()
	go hotspotRecover() // 0.2.3: hotspot left on by a crash / forced quit
	go hotspotWatch()
	token = newToken()

	ln, port, err := listen()
	if err != nil {
		fatalBox("拾音无法启动：" + err.Error())
		return
	}
	ui := &uiHandle{}
	srv := &http.Server{Handler: newMux(ui), ReadHeaderTimeout: 20 * time.Second}
	go srv.Serve(ln)
	os.WriteFile(filepath.Join(dataDir, "port.txt"), []byte(fmt.Sprint(port)), 0o644)
	writeNPPort(port) // 0.3.0: %LOCALAPPDATA%\Shiyin\np-port.txt for 驿

	if !getConfig().Scanned {
		startScan()
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
	deskURL = fmt.Sprintf("http://127.0.0.1:%d/desk?t=%s", port, token)
	log.Printf("拾音 %s listening on %s, data %s", appVersion, url, dataDir)
	runUI(ui, url)
	hotspotOnExit()
}
