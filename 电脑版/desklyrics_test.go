package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormDesk(t *testing.T) {
	p := normDesk(DeskPrefs{})
	if p.Size != deskSizeDef || p.Color != "rose" || p.On || p.Locked || p.Pos {
		t.Errorf("defaults: %+v", p)
	}
	if p := normDesk(DeskPrefs{Size: 5, Color: "bogus"}); p.Size != deskSizeMin || p.Color != "rose" {
		t.Errorf("clamp low / bad color: %+v", p)
	}
	if p := normDesk(DeskPrefs{Size: 500, Color: "ink"}); p.Size != deskSizeMax || p.Color != "ink" {
		t.Errorf("clamp high: %+v", p)
	}
	if p := normDesk(DeskPrefs{Pos: true, W: 0}); p.Pos {
		t.Error("position without a width must be ignored")
	}
}

func TestDeskHeight(t *testing.T) {
	if h := deskHeightCSS(36); h != 36+2*47+14 {
		t.Errorf("deskHeightCSS(36)=%d", h)
	}
	if deskHeightCSS(64) <= deskHeightCSS(20) {
		t.Error("height must grow with the font size")
	}
}

func TestClampDeskRect(t *testing.T) {
	// work area of a second monitor at x 1920..3840, y 0..1040
	cases := []struct{ x, y, w, h, wx, wy, ww int32 }{
		{2000, 800, 900, 150, 2000, 800, 900},  // inside: unchanged
		{3500, 1000, 900, 150, 2940, 890, 900}, // off the right/bottom edge: pulled back
		{100, -50, 900, 150, 1920, 0, 900},     // left of the monitor / above the top
		{1900, 10, 2500, 150, 1920, 10, 1920},  // wider than the screen: shrunk
	}
	for _, c := range cases {
		x, y, w := clampDeskRect(c.x, c.y, c.w, c.h, 1920, 0, 3840, 1040)
		if x != c.wx || y != c.wy || w != c.ww {
			t.Errorf("clamp(%d,%d,%d) = %d,%d,%d want %d,%d,%d", c.x, c.y, c.w, x, y, w, c.wx, c.wy, c.ww)
		}
	}
}

func TestDeskLyricsAPI(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = old }()
	prefsMu.Lock()
	prefs = map[string]json.RawMessage{}
	prefsMu.Unlock()
	token = "tok"
	srv := httptest.NewServer(newMux(&uiHandle{}))
	defer srv.Close()
	do := func(method, path, body string) map[string]any {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-Token", "tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		if resp.StatusCode != 200 {
			t.Fatalf("%s %s → %d %v", method, path, resp.StatusCode, m)
		}
		return m
	}
	// unauthenticated → 401
	if r, _ := http.Get(srv.URL + "/api/desklyrics"); r.StatusCode != 401 {
		t.Errorf("no token: %d", r.StatusCode)
	}
	s := do("GET", "/api/desklyrics", "")
	v0 := s["v"].(float64)
	if p := s["prefs"].(map[string]any); p["on"] != false || p["size"].(float64) != deskSizeDef {
		t.Errorf("initial prefs %v", p)
	}

	// prefs saved to prefs.json (deskLyrics), clamped, partial updates keep the rest
	do("POST", "/api/desklyrics/prefs", `{"on":true,"size":40,"color":"sky"}`)
	p := do("POST", "/api/desklyrics/prefs", `{"locked":true,"size":999}`)
	if p["on"] != true || p["locked"] != true || p["size"].(float64) != deskSizeMax || p["color"] != "sky" {
		t.Errorf("merged prefs %v", p)
	}
	b, _ := os.ReadFile(filepath.Join(dataDir, "prefs.json"))
	var saved map[string]DeskPrefs
	json.Unmarshal(b, &saved)
	if d := saved["deskLyrics"]; !d.On || !d.Locked || d.Color != "sky" {
		t.Errorf("prefs.json deskLyrics = %+v (%s)", d, b)
	}
	// 0.2.5 Ctrl+Alt+L = toggleLock (locked → unlocked → locked); off → no-op; hotkey in the snapshot
	if p := do("POST", "/api/desklyrics/prefs", `{"toggleLock":true}`); p["locked"] != false || p["on"] != true {
		t.Errorf("toggle 1: %v", p)
	}
	if p := do("POST", "/api/desklyrics/prefs", `{"toggleLock":true}`); p["locked"] != true {
		t.Errorf("toggle 2: %v", p)
	}
	do("POST", "/api/desklyrics/prefs", `{"on":false}`)
	if p, ok := deskToggleLock(); ok || p.Locked != true {
		t.Errorf("toggle while off must do nothing: %v %+v", ok, p)
	}
	do("POST", "/api/desklyrics/prefs", `{"on":true}`)
	if s := do("GET", "/api/desklyrics", ""); s["hotkey"] != deskHotkey {
		t.Errorf("hotkey %v", s["hotkey"])
	}
	updateDesk(func(p *DeskPrefs) { p.Pos, p.X, p.Y, p.W = true, 10, 20, 800 }, false)
	if p := do("POST", "/api/desklyrics/prefs", `{"resetPos":true}`); p["pos"] != false {
		t.Errorf("resetPos: %v", p)
	}

	// long-poll wakes up on a now-playing update
	done := make(chan map[string]any)
	cur := do("GET", "/api/desklyrics", "")["v"].(float64)
	if cur <= v0 {
		t.Error("version did not advance on prefs change")
	}
	go func() { done <- do("GET", "/api/desklyrics?since="+ftoa(cur)+"&wait=5", "") }()
	time.Sleep(150 * time.Millisecond)
	do("POST", "/api/desklyrics/now", `{"id":7,"title":"十年","playing":true,"pos":12.5,"at":1,"offset":500,"lines":[{"t":1,"text":"a"},{"t":3,"text":"b"}]}`)
	select {
	case s := <-done:
		now := s["now"].(map[string]any)
		if now["title"] != "十年" || len(now["lines"].([]any)) != 2 || now["offset"].(float64) != 500 {
			t.Errorf("now %v", now)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long-poll did not wake up")
	}
	// a light update (no lines) for the same song keeps the lyrics; a new song without lines drops them
	do("POST", "/api/desklyrics/now", `{"id":7,"playing":false,"pos":13}`)
	if now := do("GET", "/api/desklyrics", "")["now"].(map[string]any); now["lines"] == nil || now["playing"] != false {
		t.Errorf("same song light update: %v", now)
	}
	do("POST", "/api/desklyrics/now", `{"id":8,"title":"x","playing":true,"pos":0}`)
	if now := do("GET", "/api/desklyrics", "")["now"].(map[string]any); now["lines"] != nil {
		t.Errorf("new song kept old lyrics: %v", now)
	}

	// toolbar commands are queued for the main window; bad ones refused
	do("POST", "/api/desklyrics/cmd", `{"cmd":"next"}`)
	s = do("GET", "/api/desklyrics", "")
	cmds := s["cmds"].([]any)
	if last := cmds[len(cmds)-1].(map[string]any); last["cmd"] != "next" || last["n"].(float64) != s["cmdN"].(float64) {
		t.Errorf("cmds %v", cmds)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/desklyrics/cmd", strings.NewReader(`{"cmd":"rm -rf"}`))
	req.Header.Set("X-Token", "tok")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 400 {
		t.Errorf("bad cmd accepted: %d", r.StatusCode)
	}
	// /desk swaps the token for the cookie and serves desk.html
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, _ := c.Get(srv.URL + "/desk?t=tok")
	if r.StatusCode != 302 || r.Header.Get("Location") != "/desk.html" || !strings.Contains(r.Header.Get("Set-Cookie"), "shiyin_t=tok") {
		t.Errorf("/desk: %d %v", r.StatusCode, r.Header)
	}
}

func ftoa(f float64) string { b, _ := json.Marshal(f); return string(b) }

func TestDeskLockHit(t *testing.T) {
	pill := deskPillDef(880) // 425..455 × 4..32 CSS px
	if pill != [4]int32{425, 4, 455, 32} {
		t.Fatalf("pill %v", pill)
	}
	// window at (100,500) 880×154 @96 dpi
	cases := []struct {
		x, y      int32
		win, pill bool
	}{
		{99, 520, false, false}, {100, 500, true, false}, {979, 653, true, false}, {980, 600, false, false},
		{100 + 440, 500 + 18, true, true}, {100 + 423, 502, true, true}, {100 + 422, 518, true, false},
		{100 + 440, 500 + 35, true, false}, {100 + 300, 600, true, false},
	}
	for _, c := range cases {
		w, p := deskLockHit(c.x, c.y, 100, 500, 980, 654, pill, 96)
		if w != c.win || p != c.pill {
			t.Errorf("(%d,%d) → %v %v, want %v %v", c.x, c.y, w, p, c.win, c.pill)
		}
	}
	// 150 % scaling: window 1320×231 physical, pill 637..682 × 6..48
	if _, p := deskLockHit(1000+660, 200+30, 1000, 200, 2320, 431, deskPillDef(880), 144); !p {
		t.Error("150%: centre of the pill not hit")
	}
	if _, p := deskLockHit(1000+660, 200+60, 1000, 200, 2320, 431, deskPillDef(880), 144); p {
		t.Error("150%: below the pill hit")
	}
}

func TestDeskResizeX(t *testing.T) {
	for _, c := range []struct {
		left             bool
		dx, wantX, wantW int32
	}{
		{true, -50, 50, 950}, {true, 100, 200, 800}, {true, 700, 640, 360}, {true, -500, 0, 1000},
		{false, 50, 100, 950}, {false, -600, 100, 360}, {false, 2000, 100, 1820},
	} {
		x, w := deskResizeX(c.left, 100, 1000, c.dx, 360, 0, 1920)
		if x != c.wantX || w != c.wantW {
			t.Errorf("left=%v dx=%d → %d,%d want %d,%d", c.left, c.dx, x, w, c.wantX, c.wantW)
		}
	}
}
