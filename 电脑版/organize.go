package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 整理到拾音文件夹: move songs (and their same-name .lrc) from the extra folders into the main folder.
// Port of the phone's Library.organize(): same size already there = duplicate (skipped), name clash = " (n)".

type OrganizeStatus struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Moved   int    `json:"moved"`
	Dup     int    `json:"dup"`
	Failed  int    `json:"failed"`
	Error   string `json:"error,omitempty"`
	Auto    bool   `json:"auto,omitempty"`
	Seq     int64  `json:"seq"`
}

var (
	orgMu     sync.Mutex
	orgStatus OrganizeStatus
)

func getOrganizeStatus() OrganizeStatus {
	orgMu.Lock()
	defer orgMu.Unlock()
	return orgStatus
}

func underDir(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func outsideHome() []*Track {
	home := getConfig().MusicDir
	libMu.RLock()
	defer libMu.RUnlock()
	var out []*Track
	for _, t := range lib.Tracks {
		if !underDirFold(t.Path, home) {
			out = append(out, t)
		}
	}
	return out
}

// underDirFold: Windows paths are case-insensitive.
func underDirFold(path, dir string) bool {
	return underDir(strings.ToLower(path), strings.ToLower(dir))
}

func startOrganize(auto bool) bool {
	orgMu.Lock()
	if orgStatus.Running {
		orgMu.Unlock()
		return false
	}
	orgStatus = OrganizeStatus{Running: true, Auto: auto, Seq: orgStatus.Seq}
	orgMu.Unlock()
	go func() {
		moved, dup, failed, errMsg := organize(func(done, total int) {
			orgMu.Lock()
			orgStatus.Done, orgStatus.Total = done, total
			orgMu.Unlock()
		})
		if moved > 0 {
			if startScan() {
				waitScan()
			}
		}
		orgMu.Lock()
		orgStatus.Running = false
		orgStatus.Moved, orgStatus.Dup, orgStatus.Failed, orgStatus.Error = moved, dup, failed, errMsg
		orgStatus.Seq++
		orgMu.Unlock()
		if errMsg == "" {
			updateConfig(func(c *Config) { c.AutoOrganize = true }) // like the phone: organizing once turns on auto-organize
		}
	}()
	return true
}

func organize(progress func(done, total int)) (moved, dup, failed int, errMsg string) {
	home := getConfig().MusicDir
	if err := os.MkdirAll(home, 0o755); err != nil {
		return 0, 0, 0, "无法写入拾音文件夹：" + err.Error()
	}
	todo := outsideHome()
	for i, t := range todo {
		progress(i, len(todo))
		src := t.Path
		st, err := os.Stat(src)
		if err != nil {
			continue
		}
		name := filepath.Base(src)
		dest := filepath.Join(home, name)
		if dst, err := os.Stat(dest); err == nil {
			if dst.Size() == st.Size() {
				dup++
				continue
			}
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			for n := 2; ; n++ {
				dest = filepath.Join(home, base+" ("+strconv.Itoa(n)+")"+ext)
				if _, err := os.Stat(dest); err != nil {
					break
				}
			}
		}
		if err := moveFile(src, dest); err != nil {
			log.Printf("organize: %s: %v", src, err)
			failed++
			continue
		}
		moved++
		sBase := strings.TrimSuffix(src, filepath.Ext(src))
		dBase := strings.TrimSuffix(dest, filepath.Ext(dest))
		for _, ext := range []string{".lrc", ".LRC", ".Lrc"} {
			l := sBase + ext
			if _, err := os.Stat(l); err == nil {
				dl := dBase + ".lrc"
				if _, err := os.Stat(dl); err != nil {
					moveFile(l, dl)
				}
				break
			}
		}
	}
	progress(len(todo), len(todo))
	return
}

// moveFile renames, or copies + deletes across drives. If the source can't be deleted, the copy is removed.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		in.Close()
		return err
	}
	n, err := io.Copy(out, in)
	in.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	st, serr := os.Stat(src)
	if err != nil || serr != nil || n != st.Size() {
		os.Remove(dest)
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	if st, err := os.Stat(src); err == nil {
		os.Chtimes(dest, st.ModTime(), st.ModTime())
	}
	if err := os.Remove(src); err != nil {
		os.Remove(dest)
		return err
	}
	return nil
}
