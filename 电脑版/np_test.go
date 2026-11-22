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

func TestNPNow(t *testing.T) {
	at := time.Unix(1000, 0)
	s := npNow(npState{Playing: true, Pos: 10, Dur: 200}, at, at.Add(5*time.Second))
	if s.Pos != 15 {
		t.Errorf("playing pos %v", s.Pos)
	}
	s = npNow(npState{Playing: false, Pos: 10}, at, at.Add(5*time.Second))
	if s.Pos != 10 {
		t.Errorf("paused pos %v", s.Pos)
	}
	s = npNow(npState{Playing: true, Pos: 198, Dur: 200}, at, at.Add(9*time.Second))
	if s.Pos != 200 {
		t.Errorf("clamped pos %v", s.Pos)
	}
	s = npNow(npState{Playing: true, Pos: 0, Rate: 2}, at, at.Add(3*time.Second))
	if s.Pos != 6 {
		t.Errorf("rate pos %v", s.Pos)
	}
}

func TestNPLocalOnly(t *testing.T) {
	mk := func(remote, host string, hdr map[string]string) *http.Request {
		r := httptest.NewRequest("GET", "http://"+host+"/api/np", nil)
		r.RemoteAddr, r.Host = remote, host
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	cases := []struct {
		r    *http.Request
		want bool
	}{
		{mk("127.0.0.1:5000", "127.0.0.1:18530", nil), true},
		{mk("[::1]:5000", "localhost:18530", nil), true},
		{mk("192.168.0.5:5000", "127.0.0.1:18530", nil), false},
		{mk("127.0.0.1:5000", "evil.example:18530", nil), false}, // DNS rebinding
		{mk("127.0.0.1:5000", "127.0.0.1:18530", map[string]string{"Origin": "https://evil.example"}), false},
		{mk("127.0.0.1:5000", "127.0.0.1:18530", map[string]string{"Sec-Fetch-Site": "cross-site"}), false},
		{mk("127.0.0.1:5000", "127.0.0.1:18530", map[string]string{"Sec-Fetch-Site": "none"}), true},
	}
	for i, c := range cases {
		if got := npLocalOnly(c.r); got != c.want {
			t.Errorf("case %d: %v want %v", i, got, c.want)
		}
	}
}

func TestNPAPI(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = old }()
	prefsMu.Lock()
	prefs = map[string]json.RawMessage{}
	prefsMu.Unlock()
	token = "tok"
	srv := httptest.NewServer(newMux(&uiHandle{}))
	defer srv.Close()
	req := func(method, path, body string, tok bool) (int, map[string]any) {
		r, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if tok {
			r.Header.Set("X-Token", "tok")
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	// push needs the token
	if c, _ := req("POST", "/api/np/now", `{"title":"x"}`, false); c != 401 {
		t.Errorf("np/now without token: %d", c)
	}
	if c, _ := req("POST", "/api/np/now", `{"title":"一念之间","artist":"陶喆","album":"太平盛世","playing":true,"pos":30,"dur":250}`, true); c != 200 {
		t.Fatalf("np/now: %d", c)
	}
	// read needs no token
	c, m := req("GET", "/api/np", "", false)
	if c != 200 || m["app"] != "shiyin" || m["title"] != "一念之间" || m["artist"] != "陶喆" || m["album"] != "太平盛世" || m["playing"] != true || m["dur"].(float64) != 250 || m["pos"].(float64) < 30 {
		t.Errorf("np: %d %v", c, m)
	}
	// browser page → 403
	r, _ := http.NewRequest("GET", srv.URL+"/api/np", nil)
	r.Header.Set("Origin", "https://evil.example")
	if resp, _ := http.DefaultClient.Do(r); resp.StatusCode != 403 {
		t.Errorf("origin: %d", resp.StatusCode)
	}
	// ctl → queued for the main window as a desk command
	before := deskSnapshot()["cmdN"].(int64)
	for _, d := range []string{"toggle", "play", "pause", "next", "prev"} {
		if c, _ := req("POST", "/api/np/ctl?do="+d, "", false); c != 200 {
			t.Errorf("ctl %s: %d", d, c)
		}
	}
	snap := deskSnapshot()
	cmds := snap["cmds"].([]deskCmd)
	if snap["cmdN"].(int64) != before+5 || cmds[len(cmds)-1].Cmd != "prev" || cmds[len(cmds)-4].Cmd != "play" {
		t.Errorf("cmds %v", cmds)
	}
	if c, _ := req("POST", "/api/np/ctl?do=rm", "", false); c != 400 {
		t.Errorf("bad cmd: %d", c)
	}
	if c, _ := req("GET", "/api/np/ctl?do=next", "", false); c != 405 {
		t.Errorf("GET ctl: %d", c)
	}
}

func TestNPPortFile(t *testing.T) {
	d := t.TempDir()
	t.Setenv("SHIYIN_NP_DIR", filepath.Join(d, "Shiyin"))
	writeNPPort(18531)
	b, err := os.ReadFile(filepath.Join(d, "Shiyin", "np-port.txt"))
	if err != nil || string(b) != "18531" {
		t.Errorf("port file %q %v", b, err)
	}
	t.Setenv("SHIYIN_NP_DIR", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\fen\AppData\Local`)
	if got := npPortDir(); got != filepath.Join(`C:\Users\fen\AppData\Local`, "Shiyin") {
		t.Errorf("dir %s", got)
	}
}
