package main

// USB fallback (0.2.1): the phone (拾音 ≥ 2.2, setting 「导出歌单和改名到拾音文件夹」) keeps
// 拾音数据.json + .拾音封面/ in its 拾音 music folder. When the user copies that folder to the PC by
// cable, the scan finds the file in any library folder and merges it like a phone import:
// display names → meta.Phone, custom covers → dataDir/covers, playlists (incl. 我喜欢) merged by key.
// Idempotent (a file whose content was already merged is skipped; merges only add) and never
// touches audio files. Format: shiyin-sync-protocol.md「USB 数据文件」.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	usbDataFile = "拾音数据.json"
	usbCoverDir = ".拾音封面"
)

type usbTrack struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Src      string `json:"src"`
	Cover    string `json:"cover"`
	CoverVer int64  `json:"coverVer"`
}

type usbDoc struct {
	App       string              `json:"app"`
	Kind      string              `json:"kind"`
	Format    int                 `json:"format"`
	Device    string              `json:"device"`
	Version   string              `json:"version"`
	Updated   int64               `json:"updated"`
	Tracks    map[string]usbTrack `json:"tracks"`
	Playlists []PhonePlaylist     `json:"playlists"`
}

// mergeUSBData merges every 拾音数据.json found by the scan. Returns a short note for the UI ("" if nothing new).
func mergeUSBData(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	done := map[string]string{}
	prefGet("usbData", &done)
	names, covers, pls, files := 0, 0, 0, 0
	byKey := tracksByKey()
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		pk := strings.ToLower(filepath.Clean(p))
		if done[pk] == h {
			continue // already merged this exact content
		}
		var d usbDoc
		if err := json.Unmarshal(b, &d); err != nil || d.App != "shiyin" || d.Kind != "shiyin-data" {
			log.Printf("usbdata: %s: not a 拾音 data file (%v)", p, err)
			continue
		}
		n, c := mergeUSBTracks(filepath.Dir(p), d.Tracks, byKey)
		names += n
		covers += c
		pls += importPlaylists(d.Playlists)
		files++
		done[pk] = h
	}
	if files == 0 {
		return ""
	}
	saveMeta()
	prefSet("usbData", done)
	parts := []string{}
	if names > 0 {
		parts = append(parts, fmt.Sprintf("%d 首中文名", names))
	}
	if covers > 0 {
		parts = append(parts, fmt.Sprintf("%d 张封面", covers))
	}
	if pls > 0 {
		parts = append(parts, fmt.Sprintf("%d 个歌单", pls))
	}
	if len(parts) == 0 {
		return ""
	}
	return "已读取手机拾音数据：" + strings.Join(parts, "、")
}

func mergeUSBTracks(dir string, tracks map[string]usbTrack, have map[string][]*Track) (names, covers int) {
	for key, t := range tracks {
		key = strings.TrimSpace(key)
		if key == "" || !strings.Contains(key, "|") {
			continue
		}
		var coverName string
		var coverV int64
		if t.Cover != "" && !strings.ContainsAny(t.Cover, `/\`) && !strings.Contains(t.Cover, "..") {
			src := filepath.Join(dir, usbCoverDir, t.Cover)
			if b, err := os.ReadFile(src); err == nil && len(b) > 100 {
				if _, ok := imageExt(b); ok {
					name := "phone_" + hashStr(key) + ".jpg"
					dst := filepath.Join(dataDir, "covers", name)
					old, _ := os.ReadFile(dst)
					if string(old) != string(b) {
						os.MkdirAll(filepath.Dir(dst), 0o755)
						if os.WriteFile(dst+".tmp", b, 0o644) == nil && os.Rename(dst+".tmp", dst) == nil {
							coverName, coverV = name, time.Now().UnixNano()/1e6
							if len(have[key]) > 0 {
								covers++
							}
						}
					}
				}
			}
		}
		metaMu.Lock()
		m := meta.Phone[key]
		before := m
		if v := strings.TrimSpace(t.Title); v != "" {
			m.Title = v
		}
		if v := strings.TrimSpace(t.Artist); v != "" {
			m.Artist = v
		}
		if v := strings.TrimSpace(t.Album); v != "" {
			m.Album = v
		}
		if coverName != "" {
			m.Cover, m.CoverV = coverName, coverV
		}
		if m != before {
			meta.Phone[key] = m
			if len(have[key]) > 0 && (m.Title != before.Title || m.Artist != before.Artist || m.Album != before.Album) {
				names++
			}
		}
		metaMu.Unlock()
	}
	return
}
