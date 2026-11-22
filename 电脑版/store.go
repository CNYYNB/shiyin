package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// ---------- paths ----------

var dataDir string

func initDataDir() {
	if d := os.Getenv("SHIYIN_DATA"); d != "" {
		dataDir = d
	} else {
		base, err := os.UserConfigDir() // %APPDATA% on Windows
		if err != nil || base == "" {
			base, _ = os.UserHomeDir()
		}
		dataDir = filepath.Join(base, "拾音")
	}
	for _, sub := range []string{"", "covers", "lyrics"} {
		os.MkdirAll(filepath.Join(dataDir, sub), 0o755)
	}
}

func defaultMusicDir() string {
	if d := os.Getenv("SHIYIN_MUSIC"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Music", "拾音")
}

// ---------- json helpers ----------

var fileLock sync.Mutex

func loadJSON(name string, v any) bool {
	b, err := os.ReadFile(filepath.Join(dataDir, name))
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// saveJSON writes atomically (tmp + rename) so a crash never leaves half a file.
func saveJSON(name string, v any) error {
	fileLock.Lock()
	defer fileLock.Unlock()
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	p := filepath.Join(dataDir, name)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func hashStr(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// ---------- key (must match the phone exactly) ----------
// key = lowercase( fileName without extension, trailing " (n)" removed, trimmed ) + "|" + size

var dupSuffix = regexp.MustCompile(` \(\d+\)$`)

func trackKey(fileName string, size int64) string {
	base := fileName
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = dupSuffix.ReplaceAllString(base, "")
	base = strings.ToLower(strings.TrimSpace(base))
	return base + "|" + strconv.FormatInt(size, 10)
}

// ---------- config ----------

type Config struct {
	MusicDir     string   `json:"musicDir"`
	ExtraDirs    []string `json:"extraDirs"`
	T2S          bool     `json:"t2s"`
	OnlineLyrics bool     `json:"onlineLyrics"`
	Scanned      bool     `json:"scanned"`
	LastPhone    string   `json:"lastPhone"` // ip:port of last connected phone
	Theme        string   `json:"theme"`     // dark | light | system
	AutoOrganize bool     `json:"autoOrganize"`
}

var (
	cfgMu sync.Mutex
	cfg   Config
)

func loadConfig() {
	cfg = Config{MusicDir: defaultMusicDir(), T2S: true, OnlineLyrics: true, Theme: "dark"}
	loadJSON("config.json", &cfg)
	if cfg.Theme != "light" && cfg.Theme != "system" {
		cfg.Theme = "dark"
	}
	if cfg.MusicDir == "" {
		cfg.MusicDir = defaultMusicDir()
	}
	os.MkdirAll(cfg.MusicDir, 0o755)
}

func getConfig() Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c := cfg
	c.ExtraDirs = append([]string(nil), cfg.ExtraDirs...)
	return c
}

func updateConfig(f func(c *Config)) Config {
	cfgMu.Lock()
	f(&cfg)
	c := cfg
	cfgMu.Unlock()
	saveJSON("config.json", c)
	return c
}

func libraryDirs() []string {
	c := getConfig()
	out := []string{c.MusicDir}
	for _, d := range c.ExtraDirs {
		if d != "" && !containsFold(out, d) {
			out = append(out, d)
		}
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(filepath.Clean(x), filepath.Clean(s)) {
			return true
		}
	}
	return false
}

// ---------- metadata (display overrides) ----------

type Meta struct {
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	Cover  string `json:"cover,omitempty"` // file name inside dataDir/covers
	CoverV int64  `json:"coverV,omitempty"`
	Src    string `json:"src,omitempty"`   // user edits: "online" (自动识别) | "manual"
	Tried  int64  `json:"tried,omitempty"` // automatic Chinese-name lookup ran and found nothing
}

func (m Meta) empty() bool { return m == (Meta{}) }
func (m Meta) named() bool { return m.Title != "" || m.Artist != "" || m.Album != "" }

type MetaStore struct {
	Phone map[string]Meta `json:"phone"` // from 「从手机导入」
	User  map[string]Meta `json:"user"`  // the user's own edits on the PC (win over phone)
}

var (
	metaMu sync.Mutex
	meta   MetaStore
)

func loadMeta() {
	loadJSON("meta.json", &meta)
	if meta.Phone == nil {
		meta.Phone = map[string]Meta{}
	}
	if meta.User == nil {
		meta.User = map[string]Meta{}
	}
}

func saveMeta() {
	metaMu.Lock()
	b, _ := json.Marshal(meta)
	metaMu.Unlock()
	var cp MetaStore
	json.Unmarshal(b, &cp)
	saveJSON("meta.json", cp)
}

// ---------- playlists ----------

type Playlist struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Fav     bool     `json:"fav,omitempty"`
	Keys    []string `json:"keys"`
	Created int64    `json:"created"`
}

var (
	plMu      sync.Mutex
	playlists []Playlist
)

func loadPlaylists() {
	loadJSON("playlists.json", &playlists)
	hasFav := false
	for _, p := range playlists {
		if p.Fav {
			hasFav = true
		}
	}
	if !hasFav {
		playlists = append([]Playlist{{ID: "fav", Name: "我喜欢", Fav: true, Keys: []string{}}}, playlists...)
	}
}

func savePlaylists() {
	plMu.Lock()
	cp := append([]Playlist(nil), playlists...)
	plMu.Unlock()
	saveJSON("playlists.json", cp)
}

// ---------- UI prefs (sort, lyric offsets, update check, …) ----------

var (
	prefsMu sync.Mutex
	prefs   = map[string]json.RawMessage{}
)

func loadPrefs() {
	loadJSON("prefs.json", &prefs)
	if prefs == nil {
		prefs = map[string]json.RawMessage{}
	}
}

func getPrefs() map[string]json.RawMessage {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	cp := make(map[string]json.RawMessage, len(prefs))
	for k, v := range prefs {
		cp[k] = v
	}
	return cp
}

// mergePrefs sets the given keys (a JSON null deletes a key).
func mergePrefs(in map[string]json.RawMessage) map[string]json.RawMessage {
	prefsMu.Lock()
	for k, v := range in {
		if string(v) == "null" {
			delete(prefs, k)
		} else {
			prefs[k] = v
		}
	}
	prefsMu.Unlock()
	cp := getPrefs()
	saveJSON("prefs.json", cp)
	return cp
}

func prefGet(k string, v any) bool {
	prefsMu.Lock()
	b, ok := prefs[k]
	prefsMu.Unlock()
	return ok && json.Unmarshal(b, v) == nil
}

func prefSet(k string, v any) {
	b, _ := json.Marshal(v)
	mergePrefs(map[string]json.RawMessage{k: b})
}
