package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Display-name edits and covers (port of the phone's applyMatch / Covers.setCustom). Music files are never modified.

func sameRawAlbum(a, b *Track) bool {
	if a.Album == "" || a.Album != b.Album {
		return false
	}
	return a.Artist == b.Artist || filepath.Dir(a.Path) == filepath.Dir(b.Path)
}

func allTracks() []*Track {
	libMu.RLock()
	defer libMu.RUnlock()
	return append([]*Track(nil), lib.Tracks...)
}

// hasNames: the track already shows names chosen by the user / the phone, so spreading must leave it alone.
func hasNames(key string) bool {
	return meta.User[key].named() || meta.Phone[key].named()
}

type MetaReq struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Src    string `json:"src"`    // manual | online
	Spread bool   `json:"spread"` // 同步到同歌手、同专辑的其他歌曲
	Art    string `json:"art"`    // cover URL from 联网识别
	Cover  string `json:"cover"`  // "" | "ifMissing" | "always"
}

// applyMeta sets the names of t; with Spread, other not-yet-renamed songs of the same album get the album name,
// and of the same (non-Chinese) artist get the Chinese artist name. Returns how many songs changed.
func applyMeta(t *Track, r MetaReq) int {
	title, artist, album := strings.TrimSpace(r.Title), strings.TrimSpace(r.Artist), strings.TrimSpace(r.Album)
	if artist == "未知歌手" {
		artist = ""
	}
	if album == "未知专辑" {
		album = ""
	}
	src := r.Src
	if src != "online" {
		src = "manual"
	}
	norm := func(v, raw string) string { // equal to the file's own tag = no override
		if v == raw {
			return ""
		}
		return v
	}
	n := 1
	metaMu.Lock()
	m := meta.User[t.Key]
	m.Title, m.Artist, m.Album = norm(title, t.Title), norm(artist, t.Artist), norm(album, t.Album)
	m.Src, m.Tried = src, 0
	if !m.named() {
		m.Src = ""
	}
	setMetaLocked(t.Key, m)
	if r.Spread {
		for _, u := range allTracks() {
			if u.Key == t.Key || hasNames(u.Key) {
				continue
			}
			um := meta.User[u.Key]
			ch := false
			if album != "" && sameRawAlbum(u, t) && album != u.Album {
				um.Album, ch = album, true
			}
			if artist != "" && u.Artist == t.Artist && u.Artist != "" && !hasCjk(u.Artist) && hasCjk(artist) {
				um.Artist, ch = artist, true
			}
			if ch {
				um.Src = src
				setMetaLocked(u.Key, um)
				n++
			}
		}
	}
	metaMu.Unlock()
	saveMeta()
	if r.Art != "" && r.Cover != "" {
		targets := []*Track{t}
		if r.Cover == "ifMissing" {
			targets = nil
			for _, u := range allTracks() {
				if (u == t || sameRawAlbum(u, t)) && !viewOf(u).Cover {
					targets = append(targets, u)
				}
			}
		}
		if len(targets) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			if b, err := downloadImage(ctx, r.Art); err == nil {
				setCustomCover(targets, b)
			}
			cancel()
		}
	}
	return n
}

func setMetaLocked(key string, m Meta) {
	if m.empty() {
		delete(meta.User, key)
	} else {
		meta.User[key] = m
	}
}

func markTried(t *Track) {
	metaMu.Lock()
	m := meta.User[t.Key]
	m.Tried = time.Now().UnixMilli()
	meta.User[t.Key] = m
	metaMu.Unlock()
	saveMeta()
}

// clearAutoMeta: 撤销自动识别 — drops every online name override (manual edits and covers stay).
func clearAutoMeta() int {
	n := 0
	metaMu.Lock()
	for k, m := range meta.User {
		if m.Src == "online" && m.named() {
			m.Title, m.Artist, m.Album, m.Src = "", "", "", ""
			n++
		}
		m.Tried = 0
		setMetaLocked(k, m)
	}
	metaMu.Unlock()
	saveMeta()
	return n
}

func resetMeta(t *Track) {
	metaMu.Lock()
	m := meta.User[t.Key]
	m.Title, m.Artist, m.Album, m.Src, m.Tried = "", "", "", "", 0
	setMetaLocked(t.Key, m)
	metaMu.Unlock()
	saveMeta()
}

func imageExt(b []byte) (string, bool) {
	switch ct := http.DetectContentType(b); ct {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	case "image/bmp":
		return ".bmp", true
	default:
		return "", false
	}
}

func setCustomCover(ts []*Track, b []byte) error {
	ext, ok := imageExt(b)
	if !ok {
		return errBadImage
	}
	now := time.Now().UnixMilli()
	metaMu.Lock()
	defer func() { metaMu.Unlock(); saveMeta() }()
	for _, t := range ts {
		name := "user_" + hashStr(t.Key) + ext
		if err := os.WriteFile(filepath.Join(dataDir, "covers", name), b, 0o644); err != nil {
			return err
		}
		m := meta.User[t.Key]
		if m.Cover != "" && m.Cover != name {
			os.Remove(filepath.Join(dataDir, "covers", m.Cover))
		}
		m.Cover, m.CoverV = name, now
		meta.User[t.Key] = m
	}
	return nil
}

func clearCustomCover(ts []*Track) {
	metaMu.Lock()
	for _, t := range ts {
		m := meta.User[t.Key]
		if m.Cover != "" {
			os.Remove(filepath.Join(dataDir, "covers", m.Cover))
		}
		m.Cover, m.CoverV = "", 0
		setMetaLocked(t.Key, m)
	}
	metaMu.Unlock()
	saveMeta()
}

// cleanCovers: 清除封面缓存 — removes cover files nothing refers to any more.
func cleanCovers() int {
	used := map[string]bool{}
	metaMu.Lock()
	for _, m := range meta.User {
		used[m.Cover] = true
	}
	for _, m := range meta.Phone {
		used[m.Cover] = true
	}
	metaMu.Unlock()
	n := 0
	ents, _ := os.ReadDir(filepath.Join(dataDir, "covers"))
	for _, e := range ents {
		if !used[e.Name()] {
			if os.Remove(filepath.Join(dataDir, "covers", e.Name())) == nil {
				n++
			}
		}
	}
	return n
}

type badImage struct{}

func (badImage) Error() string { return "请选择图片文件（JPG / PNG / WebP）" }

var errBadImage error = badImage{}
