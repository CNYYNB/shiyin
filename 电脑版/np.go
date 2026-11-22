package main

// 0.3.0 now-playing bridge for 「驿」 (PC remote). WebView2 registers 拾音's media session in Windows SMTC under the
// Microsoft Edge identity, so SMTC readers cannot tell 拾音 from an Edge browser tab. 驿 asks 拾音 directly instead:
//
//   GET  /api/np                → {app:"shiyin", version, title, artist, album, playing, pos, dur, at}
//   POST /api/np/ctl?do=toggle|play|pause|next|prev → {ok:true} (run by the main window)
//
// Both are token-free but only for local programs: the peer must be loopback, the Host must be 127.0.0.1/localhost
// (no DNS rebinding) and the request must not come from a web page (no Origin / cross-site Sec-Fetch-Site),
// so a site open in a browser cannot read or drive the player. The main window pushes its state with
// POST /api/np/now (token, like every other UI call); commands go through the 桌面歌词 command queue the page
// already long-polls (desklyrics.go), so they run at once.
//
// Port discovery without knowing dataDir: %LOCALAPPDATA%\Shiyin\np-port.txt (plain decimal port, rewritten on each
// launch; dataDir\port.txt is still written as before). Override the folder with SHIYIN_NP_DIR (tests).

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type npState struct {
	Title   string  `json:"title"`
	Artist  string  `json:"artist"`
	Album   string  `json:"album"`
	Playing bool    `json:"playing"`
	Pos     float64 `json:"pos"` // seconds
	Dur     float64 `json:"dur"` // seconds, 0 = unknown
	Rate    float64 `json:"rate,omitempty"`
}

var npHub struct {
	sync.Mutex
	s  npState
	at time.Time // when s.Pos was true
	ok bool      // the main window has reported at least once
}

// npNow returns the state with the position advanced to now while playing (pure apart from the clock argument).
func npNow(s npState, at, now time.Time) npState {
	if s.Playing && !at.IsZero() {
		r := s.Rate
		if r <= 0 {
			r = 1
		}
		s.Pos += now.Sub(at).Seconds() * r
		if s.Dur > 0 && s.Pos > s.Dur {
			s.Pos = s.Dur
		}
	}
	return s
}

// npLocalOnly: loopback peer, loopback Host, not a browser page.
func npLocalOnly(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	h := r.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	if h != "127.0.0.1" && h != "localhost" && h != "::1" {
		return false
	}
	if r.Header.Get("Origin") != "" {
		return false
	}
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "none" && s != "same-origin" {
		return false
	}
	return true
}

var npCtlCmds = map[string]bool{"toggle": true, "play": true, "pause": true, "next": true, "prev": true}

// npQueueCmd hands a command to the main window (same queue as the 桌面歌词 toolbar).
func npQueueCmd(c string) int64 {
	deskHub.Lock()
	defer deskHub.Unlock()
	deskHub.cmdN++
	deskHub.cmds = append(deskHub.cmds, deskCmd{deskHub.cmdN, c})
	if len(deskHub.cmds) > 20 {
		deskHub.cmds = deskHub.cmds[len(deskHub.cmds)-20:]
	}
	deskBumpLocked()
	return deskHub.cmdN
}

func registerNP(mux *http.ServeMux, api func(string, http.HandlerFunc)) {
	mux.HandleFunc("/api/np", func(w http.ResponseWriter, r *http.Request) {
		if !npLocalOnly(r) {
			writeErr(w, 403, "forbidden")
			return
		}
		npHub.Lock()
		s, at, ok := npHub.s, npHub.at, npHub.ok
		npHub.Unlock()
		s = npNow(s, at, time.Now())
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"app": "shiyin", "version": appVersion, "ready": ok,
			"title": s.Title, "artist": s.Artist, "album": s.Album, "playing": s.Playing,
			"pos": s.Pos, "dur": s.Dur, "at": time.Now().UnixMilli()})
	})
	mux.HandleFunc("/api/np/ctl", func(w http.ResponseWriter, r *http.Request) {
		if !npLocalOnly(r) {
			writeErr(w, 403, "forbidden")
			return
		}
		if r.Method != "POST" {
			writeErr(w, 405, "POST only")
			return
		}
		c := r.URL.Query().Get("do")
		if !npCtlCmds[c] {
			writeErr(w, 400, "do must be toggle|play|pause|next|prev")
			return
		}
		writeJSON(w, map[string]any{"ok": true, "n": npQueueCmd(c)})
	})
	// main window → Go (token): full state on every change
	api("/api/np/now", func(w http.ResponseWriter, r *http.Request) {
		var in npState
		if (r.Method != "POST" && r.Method != "PUT") || readBody(r, &in) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		npHub.Lock()
		npHub.s, npHub.at, npHub.ok = in, time.Now(), true
		npHub.Unlock()
		writeJSON(w, map[string]any{"ok": true})
	})
}

// npPortDir: %LOCALAPPDATA%\Shiyin (SHIYIN_NP_DIR overrides; elsewhere the user cache dir).
func npPortDir() string {
	if d := os.Getenv("SHIYIN_NP_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "Shiyin")
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "Shiyin")
	}
	return filepath.Join(dataDir, "np")
}

func writeNPPort(port int) {
	d := npPortDir()
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "np-port.txt"), []byte(fmt.Sprint(port)), 0o644)
}
