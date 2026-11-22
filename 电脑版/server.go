package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var webFS embed.FS

var token string

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func readBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

// auth: the UI gets a per-launch token in its URL, swapped for a SameSite=Strict cookie,
// so other web pages in a normal browser cannot poke the local server.
func authed(r *http.Request) bool {
	if c, err := r.Cookie("shiyin_t"); err == nil && c.Value == token {
		return true
	}
	return r.Header.Get("X-Token") == token || r.URL.Query().Get("t") == token
}

func newMux(ui *uiHandle) http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))

	serveIndex := func(w http.ResponseWriter) {
		b, _ := webFS.ReadFile("web/index.html")
		b = []byte(strings.Replace(string(b), `<html lang="zh-CN">`, `<html lang="zh-CN" data-theme="`+getConfig().Theme+`">`, 1))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/desk" { // 0.2.4 桌面歌词 window: same token → cookie swap as the main page
			if r.URL.Query().Get("t") == token {
				http.SetCookie(w, &http.Cookie{Name: "shiyin_t", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			}
			http.Redirect(w, r, "/desk.html", http.StatusFound)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			if r.URL.Query().Get("t") == token {
				http.SetCookie(w, &http.Cookie{Name: "shiyin_t", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				http.Redirect(w, r, "/", http.StatusFound) // drop the token from the address
				return
			}
			if !authed(r) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(403)
				io.WriteString(w, "<meta charset=utf-8><p style='font:16px sans-serif'>请从「拾音」程序打开。</p>")
				return
			}
			serveIndex(w)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/whoami", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"app": "shiyin-pc", "version": appVersion})
	})
	mux.HandleFunc("/api/focus", func(w http.ResponseWriter, r *http.Request) {
		if ui != nil {
			ui.focus()
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	api := func(path string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !authed(r) {
				writeErr(w, 401, "unauthorized")
				return
			}
			h(w, r)
		})
	}

	registerNP(mux, api) // 0.3.0: now-playing bridge for 驿 (np.go)

	api("/api/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": appVersion, "dataDir": dataDir, "config": getConfig(),
			"os": runtime.GOOS, "defaultMusicDir": defaultMusicDir()})
	})

	api("/api/library", func(w http.ResponseWriter, r *http.Request) {
		libMu.RLock()
		ts := append([]*Track(nil), lib.Tracks...)
		scanned := lib.Scanned
		libMu.RUnlock()
		views := make([]TrackView, 0, len(ts))
		for _, t := range ts {
			views = append(views, viewOf(t))
		}
		plMu.Lock()
		pls := append([]Playlist(nil), playlists...)
		plMu.Unlock()
		writeJSON(w, map[string]any{"tracks": views, "playlists": pls, "scanned": scanned, "scan": getScanStatus(),
			"outside": len(outsideHome()), "organize": getOrganizeStatus()})
	})

	api("/api/scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			started := startScan()
			writeJSON(w, map[string]any{"ok": true, "started": started, "scan": getScanStatus()})
			return
		}
		writeJSON(w, getScanStatus())
	})

	mux.HandleFunc("/media", func(w http.ResponseWriter, r *http.Request) {
		if !authed(r) {
			w.WriteHeader(401)
			return
		}
		t := getTrack(r.URL.Query().Get("id"))
		if t == nil {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(t.Path)
		if err != nil {
			http.Error(w, "文件不存在", 404)
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		w.Header().Set("Content-Type", mimeFor(t.Ext))
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", st.ModTime(), f) // handles Range / 206
	})

	api("/api/cover", func(w http.ResponseWriter, r *http.Request) {
		t := getTrack(r.URL.Query().Get("id"))
		if t == nil {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case "POST": // custom cover (image bytes in body); ?ids=a,b applies it to several songs (整张专辑)
			b, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
			if err != nil || len(b) < 100 {
				writeErr(w, 400, "图片无效")
				return
			}
			if err := setCustomCover(tracksOf(r.URL.Query().Get("ids"), t), b); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true, "track": viewOf(t)})
		case "DELETE":
			clearCustomCover(tracksOf(r.URL.Query().Get("ids"), t))
			writeJSON(w, map[string]any{"ok": true, "track": viewOf(t)})
		default:
			w.Header().Set("Cache-Control", "max-age=31536000, immutable")
			if p := coverFileFor(t.Key); p != "" {
				if _, err := os.Stat(p); err == nil {
					http.ServeFile(w, r, p)
					return
				}
			}
			if b, mt := embeddedCover(t.Path); len(b) > 0 {
				if mt == "" || !strings.HasPrefix(mt, "image/") {
					mt = http.DetectContentType(b)
				}
				w.Header().Set("Content-Type", mt)
				w.Write(b)
				return
			}
			if t.DirCover != "" {
				if _, err := os.Stat(t.DirCover); err == nil {
					http.ServeFile(w, r, t.DirCover)
					return
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
		}
	})

	api("/api/lyrics", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		t := getTrack(q.Get("id"))
		if t == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, getLyrics(t, q.Get("online") == "1", q.Get("force") == "1"))
	})

	api("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MetaReq
			Reset bool `json:"reset"`
		}
		if readBody(r, &req) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		t := getTrack(req.ID)
		if t == nil {
			writeErr(w, 404, "没有这首歌")
			return
		}
		n := 0
		if req.Reset || (req.Title == "" && req.Artist == "" && req.Album == "") {
			resetMeta(t)
		} else {
			n = applyMeta(t, req.MetaReq)
		}
		writeJSON(w, map[string]any{"ok": true, "track": viewOf(t), "changed": n})
	})
	api("/api/meta/tried", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string }
		readBody(r, &req)
		if t := getTrack(req.ID); t != nil {
			markTried(t)
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api("/api/meta/clearAuto", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "count": clearAutoMeta()})
	})
	api("/api/match", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		t := getTrack(q.Get("id"))
		if t == nil {
			writeErr(w, 404, "没有这首歌")
			return
		}
		title, artist := strings.TrimSpace(q.Get("title")), strings.TrimSpace(q.Get("artist"))
		if title == "" { // batch: use the file's own names
			title, artist = t.Title, t.Artist
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		items, best, err := itunesCandidates(ctx, title, artist, t.Duration)
		if items == nil {
			items = []itunesHit{}
		}
		if err != nil && len(items) == 0 {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "tooFrequent": err == errTooFrequent})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "items": items, "best": best})
	})
	api("/api/cover/url", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs []string `json:"ids"`
			URL string   `json:"url"`
		}
		if readBody(r, &req) != nil || len(req.IDs) == 0 {
			writeErr(w, 400, "bad request")
			return
		}
		ts := tracksOf(strings.Join(req.IDs, ","), nil)
		if len(ts) == 0 {
			writeErr(w, 404, "没有这首歌")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		b, err := downloadImage(ctx, req.URL)
		if err == nil {
			err = setCustomCover(ts, b)
		}
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "track": viewOf(ts[0])})
	})
	api("/api/covers/clean", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "removed": cleanCovers()})
	})

	api("/api/duration", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID       string
			Duration int64
		}
		readBody(r, &req)
		libMu.Lock()
		t := byID[req.ID]
		changed := false
		if t != nil && req.Duration > 0 {
			d := t.Duration - req.Duration
			if d < 0 {
				d = -d
			}
			if t.Duration == 0 || d > 2000 {
				t.Duration = req.Duration
				changed = true
			}
		}
		libMu.Unlock()
		if changed {
			saveLibrary()
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	api("/api/playlists", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" || r.Method == "POST" {
			var pls []Playlist
			if err := readBody(r, &pls); err != nil {
				writeErr(w, 400, "bad request")
				return
			}
			for i := range pls {
				if pls[i].Keys == nil {
					pls[i].Keys = []string{}
				}
			}
			plMu.Lock()
			playlists = pls
			plMu.Unlock()
			loadPlaylistsFavGuard()
			savePlaylists()
		}
		plMu.Lock()
		pls := append([]Playlist(nil), playlists...)
		plMu.Unlock()
		writeJSON(w, pls)
	})

	api("/api/state", func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dataDir, "state.json")
		if r.Method == "PUT" || r.Method == "POST" {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			if json.Valid(b) {
				fileLock.Lock()
				os.WriteFile(p+".tmp", b, 0o644)
				os.Rename(p+".tmp", p)
				fileLock.Unlock()
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		b, err := os.ReadFile(p)
		if err != nil || !json.Valid(b) {
			b = []byte("{}")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})

	api("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var req struct {
				MusicDir     *string   `json:"musicDir"`
				ExtraDirs    *[]string `json:"extraDirs"`
				T2S          *bool     `json:"t2s"`
				OnlineLyrics *bool     `json:"onlineLyrics"`
				Theme        *string   `json:"theme"`
				AutoOrganize *bool     `json:"autoOrganize"`
			}
			if readBody(r, &req) != nil {
				writeErr(w, 400, "bad request")
				return
			}
			if req.MusicDir != nil {
				d := strings.TrimSpace(*req.MusicDir)
				if d == "" {
					writeErr(w, 400, "文件夹不能为空")
					return
				}
				if err := os.MkdirAll(d, 0o755); err != nil {
					writeErr(w, 400, "无法使用这个文件夹："+err.Error())
					return
				}
			}
			if req.ExtraDirs != nil {
				for _, d := range *req.ExtraDirs {
					if st, err := os.Stat(d); err != nil || !st.IsDir() {
						writeErr(w, 400, "文件夹不存在："+d)
						return
					}
				}
			}
			updateConfig(func(c *Config) {
				if req.MusicDir != nil {
					c.MusicDir = filepath.Clean(strings.TrimSpace(*req.MusicDir))
				}
				if req.ExtraDirs != nil {
					c.ExtraDirs = *req.ExtraDirs
				}
				if req.T2S != nil {
					c.T2S = *req.T2S
				}
				if req.OnlineLyrics != nil {
					c.OnlineLyrics = *req.OnlineLyrics
				}
				if req.Theme != nil && (*req.Theme == "dark" || *req.Theme == "light" || *req.Theme == "system") {
					c.Theme = *req.Theme
				}
				if req.AutoOrganize != nil {
					c.AutoOrganize = *req.AutoOrganize
				}
			})
		}
		writeJSON(w, getConfig())
	})

	api("/api/fs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, listDir(r.URL.Query().Get("path")))
	})

	api("/api/reveal", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if id := q.Get("id"); id != "" {
			if t := getTrack(id); t != nil {
				revealFile(t.Path)
			}
		} else if p := q.Get("path"); p != "" {
			openFolder(p)
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	registerDeskLyrics(api, ui) // 0.2.4 桌面歌词

	api("/api/prefs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" {
			var in map[string]json.RawMessage
			if readBody(r, &in) != nil {
				writeErr(w, 400, "bad request")
				return
			}
			writeJSON(w, mergePrefs(in))
			return
		}
		writeJSON(w, getPrefs())
	})
	api("/api/theme", func(w http.ResponseWriter, r *http.Request) { // effective dark/light → window title bar
		var req struct{ Dark bool }
		readBody(r, &req)
		ui.setDark(req.Dark)
		writeJSON(w, map[string]any{"ok": true})
	})

	// ---- lyrics: manual search / save / reset ----
	api("/api/lyrics/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		title := strings.TrimSpace(q.Get("title"))
		if title == "" {
			writeErr(w, 400, "请输入歌名")
			return
		}
		items, _ := searchLyrics(title, strings.TrimSpace(q.Get("artist")))
		if items == nil {
			items = []LyricItem{}
		}
		writeJSON(w, map[string]any{"ok": true, "items": items})
	})
	api("/api/lyrics/save", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID, Text string }
		if readBody(r, &req) != nil || strings.TrimSpace(req.Text) == "" {
			writeErr(w, 400, "bad request")
			return
		}
		t := getTrack(req.ID)
		if t == nil {
			writeErr(w, 404, "没有这首歌")
			return
		}
		if err := saveManualLyrics(t, req.Text); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api("/api/lyrics/reset", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string }
		readBody(r, &req)
		if t := getTrack(req.ID); t != nil {
			resetLyrics(t)
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// ---- 整理到拾音文件夹 ----
	api("/api/organize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var req struct{ Auto bool }
			readBody(r, &req)
			if getScanStatus().Running {
				writeErr(w, 409, "正在扫描，请稍后再整理")
				return
			}
			startOrganize(req.Auto)
		}
		writeJSON(w, getOrganizeStatus())
	})

	// ---- 内置更新 ----
	api("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, checkUpdate(r.URL.Query().Get("silent") == "1"))
	})
	api("/api/update/start", func(w http.ResponseWriter, r *http.Request) {
		startUpdateDownload()
		writeJSON(w, getUpdateStatus())
	})
	api("/api/update/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, getUpdateStatus())
	})
	api("/api/update/cancel", func(w http.ResponseWriter, r *http.Request) {
		cancelUpdate()
		writeJSON(w, getUpdateStatus())
	})
	api("/api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if err := installUpdate(ui.quit); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// ---- phone import ----
	api("/api/phone/discover", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"phones": discoverPhones(2 * time.Second), "last": getConfig().LastPhone})
	})
	api("/api/phone/connect", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Host, Code string }
		if readBody(r, &req) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		lib, err := connectPhone(req.Host, req.Code)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "library": lib})
	})
	// 「手机连电脑」(reverse): listener on 18522 while the import dialog is open (it polls status as a heartbeat)
	api("/api/phone/reverse/start", func(w http.ResponseWriter, r *http.Request) {
		st, err := rev.start()
		if err != nil {
			st.Error = err.Error()
		}
		writeJSON(w, st)
	})
	api("/api/phone/reverse/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, rev.heartbeat())
	})
	// 0.2.2 电脑热点 (GET = status, POST ?on=1|0). 0.2.3: ?on=1 checks TUN/VPN adapters first
	// (needConfirm → POST ?on=1&force=1), and a hotspot 拾音 started is turned off again automatically.
	api("/api/phone/hotspot", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method != "POST":
			writeJSON(w, hotspotStatus())
		case q.Get("on") == "1":
			st := hotspotTurnOn(q.Get("force") == "1")
			st.AutoOff = int(hotspotIdleOff / time.Second)
			writeJSON(w, st)
		default:
			writeJSON(w, hotspotTurnOff("用户关闭"))
		}
	})
	// 0.2.2 网络诊断
	api("/api/phone/diag", func(w http.ResponseWriter, r *http.Request) {
		ip := strings.TrimSpace(r.URL.Query().Get("ip"))
		if h, _, err := net.SplitHostPort(ip); err == nil {
			ip = h
		}
		rep, iso := runDiag(ip)
		writeJSON(w, map[string]any{"ok": true, "report": rep, "isolation": iso})
	})
	api("/api/phone/reverse/stop", func(w http.ResponseWriter, r *http.Request) {
		if !importStatus().Running {
			rev.stop()
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api("/api/phone/reverse/connect", func(w http.ResponseWriter, r *http.Request) {
		lib, err := connectReverse()
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "library": lib})
	})
	api("/api/phone/library", func(w http.ResponseWriter, r *http.Request) {
		l := refreshPresent()
		if l == nil {
			writeErr(w, 404, "未连接")
			return
		}
		writeJSON(w, map[string]any{"ok": true, "library": l})
	})
	api("/api/phone/cover", func(w http.ResponseWriter, r *http.Request) {
		phMu.Lock()
		s := ph
		phMu.Unlock()
		if s == nil {
			http.NotFound(w, r)
			return
		}
		b, err := fetchSmall(r.Context(), s, "/api/cover", r.URL.Query().Get("key"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(b))
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(b)
	})
	api("/api/phone/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var req struct{ Keys []string }
			if readBody(r, &req) != nil {
				writeErr(w, 400, "bad request")
				return
			}
			if err := startImport(req.Keys); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
		}
		writeJSON(w, importStatus())
	})
	api("/api/phone/cancel", func(w http.ResponseWriter, r *http.Request) {
		cancelImport()
		writeJSON(w, importStatus())
	})

	return mux
}

// tracksOf resolves "id1,id2" (falls back to def).
func tracksOf(ids string, def *Track) []*Track {
	var out []*Track
	seen := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		if t := getTrack(strings.TrimSpace(id)); t != nil && !seen[t.Key] {
			seen[t.Key] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 && def != nil {
		out = []*Track{def}
	}
	return out
}

func loadPlaylistsFavGuard() {
	plMu.Lock()
	defer plMu.Unlock()
	for _, p := range playlists {
		if p.Fav {
			return
		}
	}
	playlists = append([]Playlist{{ID: "fav", Name: "我喜欢", Fav: true, Keys: []string{}}}, playlists...)
}

func mimeFor(ext string) string {
	switch ext {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "m4a", "mp4", "alac":
		return "audio/mp4"
	case "aac":
		return "audio/aac"
	case "ogg", "oga", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "webm":
		return "audio/webm"
	}
	return "application/octet-stream"
}

type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func listDir(p string) map[string]any {
	home, _ := os.UserHomeDir()
	quick := []dirEntry{{"主目录", home}, {"音乐", filepath.Join(home, "Music")}, {"下载", filepath.Join(home, "Downloads")}, {"桌面", filepath.Join(home, "Desktop")}}
	if p == "" {
		var roots []dirEntry
		if runtime.GOOS == "windows" {
			for c := 'A'; c <= 'Z'; c++ {
				d := string(c) + ":\\"
				if _, err := os.Stat(d); err == nil {
					roots = append(roots, dirEntry{string(c) + ": 盘", d})
				}
			}
		} else {
			roots = append(roots, dirEntry{"/", "/"})
		}
		return map[string]any{"path": "", "parent": nil, "dirs": roots, "quick": quick}
	}
	p = filepath.Clean(p)
	ents, err := os.ReadDir(p)
	if err != nil {
		return map[string]any{"path": p, "error": "无法打开：" + err.Error(), "dirs": []dirEntry{}, "quick": quick, "parent": parentOf(p)}
	}
	var dirs []dirEntry
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "$") {
			continue
		}
		dirs = append(dirs, dirEntry{e.Name(), filepath.Join(p, e.Name())})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	if dirs == nil {
		dirs = []dirEntry{}
	}
	return map[string]any{"path": p, "parent": parentOf(p), "dirs": dirs, "quick": quick}
}

func parentOf(p string) any {
	par := filepath.Dir(p)
	if par == p {
		return "" // drive root → drive list
	}
	return par
}

func itoa(i int) string { return strconv.Itoa(i) }

func listen() (net.Listener, int, error) {
	if p := os.Getenv("SHIYIN_PORT"); p != "" {
		ln, err := net.Listen("tcp", "127.0.0.1:"+p)
		if err != nil {
			return nil, 0, err
		}
		return ln, ln.Addr().(*net.TCPAddr).Port, nil
	}
	for port := 18530; port < 18540; port++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
		if err == nil {
			return ln, port, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}
