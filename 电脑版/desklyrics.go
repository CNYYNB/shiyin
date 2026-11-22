package main

// 0.2.4 桌面歌词: a separate borderless, transparent, always-on-top window (desklyrics_windows.go)
// showing web/desk.html. The main window owns the player and the parsed lyrics (same offset + 歌词转简体),
// pushes them here (POST /api/desklyrics/now); the desk page and the main page long-poll
// GET /api/desklyrics?since=v. Toolbar buttons on the desk page come back to the main page as cmds.
// Settings live in prefs.json "deskLyrics".

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type DeskPrefs struct {
	On     bool   `json:"on"`
	Locked bool   `json:"locked"`
	Size   int    `json:"size"`  // font size of the current line, CSS px
	Color  string `json:"color"` // preset id (deskColors)
	Pos    bool   `json:"pos"`   // X/Y/W valid (physical px, virtual-screen coordinates)
	X      int32  `json:"x"`
	Y      int32  `json:"y"`
	W      int32  `json:"w"`
}

var deskColors = []string{"rose", "sky", "gold", "green", "ink"}

// deskHotkey: the global lock/unlock hotkey ("Ctrl+Alt+L") once RegisterHotKey succeeded, "" otherwise
// (taken by another app → no hint is shown anywhere). Set on the UI thread at startup, read by the API.
var deskHotkey string

// deskPillDef: the lock-icon unlock button in CSS px (left, top, right, bottom) relative to the window, for a given window
// width; desk.html reports its real rect ("pill:l,t,r,b"), this is only the fallback (same numbers as its CSS).
func deskPillDef(wCSS int32) [4]int32 {
	const pw, ph, top = 30, 28, 4
	l := (wCSS - pw) / 2
	return [4]int32{l, top, l + pw, top + ph}
}

// deskLockHit (pure, 0.2.5): while locked, is the cursor (cx,cy, physical px) over the window [l,t,r,b] and over
// the unlock pill (CSS px relative to the window, scaled by dpi/96)? The pill gets a 2 px slack.
func deskLockHit(cx, cy, l, t, r, b int32, pill [4]int32, dpi int32) (overWin, overPill bool) {
	overWin = cx >= l && cx < r && cy >= t && cy < b
	if !overWin {
		return false, false
	}
	sc := func(v int32) int32 { return v * dpi / 96 }
	x, y := cx-l, cy-t
	overPill = x >= sc(pill[0]-2) && x < sc(pill[2]+2) && y >= sc(pill[1]-2) && y < sc(pill[3]+2)
	return
}

// deskResizeX (pure, 0.2.5): new x/width while dragging the left or right grip by dx; keeps the opposite edge fixed,
// width ≥ minW, inside the work area's [waL, waR].
func deskResizeX(left bool, l, r, dx, minW, waL, waR int32) (x, w int32) {
	if left {
		x = max(waL, min(l+dx, r-minW))
		return x, r - x
	}
	nr := min(waR, max(r+dx, l+minW))
	return l, nr - l
}

// deskToggleLock: the global hotkey. Only acts while 桌面歌词 is on (an off window has nothing to unlock).
func deskToggleLock() (DeskPrefs, bool) {
	if !getDeskPrefs().On {
		return getDeskPrefs(), false
	}
	return updateDesk(func(p *DeskPrefs) { p.Locked = !p.Locked }, true), true
}

const (
	deskSizeMin, deskSizeMax, deskSizeDef = 20, 64, 36
	deskWidthDef                          = 880 // CSS px
	deskWidthMin                          = 360
)

func normDesk(p DeskPrefs) DeskPrefs {
	if p.Size == 0 {
		p.Size = deskSizeDef
	}
	p.Size = max(deskSizeMin, min(deskSizeMax, p.Size))
	ok := false
	for _, c := range deskColors {
		ok = ok || c == p.Color
	}
	if !ok {
		p.Color = deskColors[0]
	}
	if p.Pos && p.W <= 0 {
		p.Pos = false
	}
	return p
}

// deskHeightCSS: window height for a font size (toolbar strip + two lyric rows); desk.html uses the same numbers.
func deskHeightCSS(size int) int {
	row := (size*13 + 9) / 10 // ceil(size*1.3)
	return 36 + 2*row + 14
}

// clampDeskRect keeps a w×h window fully inside the work area [l,t,r,b] (shrinking the width if needed).
func clampDeskRect(x, y, w, h, l, t, r, b int32) (int32, int32, int32) {
	if w > r-l {
		w = r - l
	}
	if x+w > r {
		x = r - w
	}
	if x < l {
		x = l
	}
	if y+h > b {
		y = b - h
	}
	if y < t {
		y = t
	}
	return x, y, w
}

func getDeskPrefs() DeskPrefs {
	var p DeskPrefs
	prefGet("deskLyrics", &p)
	return normDesk(p)
}

type deskCmd struct {
	N   int64  `json:"n"`
	Cmd string `json:"cmd"`
}

var deskHub = struct {
	sync.Mutex
	v    int64
	ch   chan struct{}
	now  map[string]json.RawMessage
	cmds []deskCmd
	cmdN int64
	ui   *uiHandle
}{ch: make(chan struct{}), now: map[string]json.RawMessage{}}

func deskBumpLocked() {
	deskHub.v++
	close(deskHub.ch)
	deskHub.ch = make(chan struct{})
}

// updateDesk changes the saved settings; apply=false only for the window reporting its own new position.
func updateDesk(f func(p *DeskPrefs), apply bool) DeskPrefs {
	deskHub.Lock()
	p := getDeskPrefs()
	f(&p)
	p = normDesk(p)
	prefSet("deskLyrics", p)
	deskBumpLocked()
	ui := deskHub.ui
	deskHub.Unlock()
	if apply && ui != nil {
		ui.deskApply(p)
	}
	return p
}

func deskSnapshot() map[string]any {
	deskHub.Lock()
	defer deskHub.Unlock()
	now := make(map[string]json.RawMessage, len(deskHub.now))
	for k, v := range deskHub.now {
		now[k] = v
	}
	return map[string]any{"v": deskHub.v, "prefs": getDeskPrefs(), "now": now,
		"cmds": append([]deskCmd{}, deskHub.cmds...), "cmdN": deskHub.cmdN, "colors": deskColors,
		"sizeMin": deskSizeMin, "sizeMax": deskSizeMax, "hotkey": deskHotkey}
}

func registerDeskLyrics(api func(string, http.HandlerFunc), ui *uiHandle) {
	deskHub.Lock()
	deskHub.ui = ui
	deskHub.Unlock()

	// GET ?since=v → returns at once if the version differs, else waits up to ?wait= seconds (max 25).
	api("/api/desklyrics", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if s := q.Get("since"); s != "" {
			since, _ := strconv.ParseInt(s, 10, 64)
			wait, _ := strconv.Atoi(q.Get("wait"))
			if wait <= 0 || wait > 25 {
				wait = 25
			}
			deskHub.Lock()
			v, ch := deskHub.v, deskHub.ch
			deskHub.Unlock()
			if v == since {
				t := time.NewTimer(time.Duration(wait) * time.Second)
				select {
				case <-ch:
				case <-t.C:
				case <-r.Context().Done():
				}
				t.Stop()
			}
		}
		writeJSON(w, deskSnapshot())
	})

	// POST (main window): now-playing fields to merge: id, title, artist, playing, pos (s), at (ms epoch),
	// offset (ms, + = lyrics earlier), rate, lines [{t,text}] | null, plain (bool).
	api("/api/desklyrics/now", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]json.RawMessage
		if r.Method != "POST" || readBody(r, &in) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		deskHub.Lock()
		if string(in["id"]) != string(deskHub.now["id"]) && in["lines"] == nil {
			delete(deskHub.now, "lines") // new song: never show the old song's lyrics
			delete(deskHub.now, "plain")
		}
		for k, v := range in {
			deskHub.now[k] = v
		}
		deskBumpLocked()
		v := deskHub.v
		deskHub.Unlock()
		writeJSON(w, map[string]any{"ok": true, "v": v})
	})

	// POST: partial settings {on, locked, size, color} (also used by the desk toolbar).
	api("/api/desklyrics/prefs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, getDeskPrefs())
			return
		}
		var in struct {
			On     *bool   `json:"on"`
			Locked *bool   `json:"locked"`
			Size   *int    `json:"size"`
			Color  *string `json:"color"`
			Reset  bool    `json:"resetPos"`
			Toggle bool    `json:"toggleLock"` // same as the Ctrl+Alt+L hotkey
		}
		if readBody(r, &in) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		if in.Toggle {
			p, _ := deskToggleLock()
			writeJSON(w, p)
			return
		}
		p := updateDesk(func(p *DeskPrefs) {
			if in.On != nil {
				p.On = *in.On
			}
			if in.Locked != nil {
				p.Locked = *in.Locked
			}
			if in.Size != nil {
				p.Size = *in.Size
			}
			if in.Color != nil {
				p.Color = *in.Color
			}
			if in.Reset {
				p.Pos = false
			}
		}, true)
		writeJSON(w, p)
	})

	// POST {cmd: prev|toggle|next} from the desk toolbar → executed by the main window.
	api("/api/desklyrics/cmd", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Cmd string }
		if r.Method != "POST" || readBody(r, &in) != nil || (in.Cmd != "prev" && in.Cmd != "toggle" && in.Cmd != "next") {
			writeErr(w, 400, "bad request")
			return
		}
		deskHub.Lock()
		deskHub.cmdN++
		deskHub.cmds = append(deskHub.cmds, deskCmd{deskHub.cmdN, in.Cmd})
		if len(deskHub.cmds) > 20 {
			deskHub.cmds = deskHub.cmds[len(deskHub.cmds)-20:]
		}
		deskBumpLocked()
		n := deskHub.cmdN
		deskHub.Unlock()
		writeJSON(w, map[string]any{"ok": true, "n": n})
	})
}
