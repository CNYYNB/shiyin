package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dhowden/tag"
	"golang.org/x/text/encoding/simplifiedchinese"
)

var audioExts = map[string]bool{
	"mp3": true, "flac": true, "m4a": true, "aac": true, "mp4": true, "ogg": true, "oga": true,
	"opus": true, "wav": true, "webm": true,
}

type Track struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Key      string `json:"key"`
	FileName string `json:"fileName"`
	Ext      string `json:"ext"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	TrackNo  int    `json:"trackNo,omitempty"`
	Duration int64  `json:"duration"`
	Size     int64  `json:"size"`
	Mtime    int64  `json:"mtime"`
	Added    int64  `json:"added"`
	EmbCover bool   `json:"embCover,omitempty"`
	EmbLyr   bool   `json:"embLyr,omitempty"`
	DirCover string `json:"dirCover,omitempty"` // cover.jpg / folder.jpg next to the file
}

var dirCoverNames = []string{"cover.jpg", "folder.jpg", "cover.png", "folder.png", "Cover.jpg", "Folder.jpg", "AlbumArtSmall.jpg"}

// findDirCover looks for cover.jpg / folder.jpg beside the song (only when it is not the big shared music folder).
func findDirCover(p string, cache map[string]string) string {
	dir := filepath.Dir(p)
	if v, ok := cache[dir]; ok {
		return v
	}
	v := ""
	for _, n := range dirCoverNames {
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && !st.IsDir() && st.Size() > 100 {
			v = filepath.Join(dir, n)
			break
		}
	}
	if cache != nil {
		cache[dir] = v
	}
	return v
}

type Library struct {
	Tracks  []*Track `json:"tracks"`
	Scanned int64    `json:"scanned"`
}

var (
	libMu sync.RWMutex
	lib   Library
	byID  = map[string]*Track{}
)

func fileExt(p string) string { return filepath.Ext(p) }

func loadLibrary() {
	loadJSON("library.json", &lib)
	reindex()
}

func reindex() {
	m := make(map[string]*Track, len(lib.Tracks))
	for _, t := range lib.Tracks {
		m[t.ID] = t
	}
	byID = m
}

func saveLibrary() {
	libMu.RLock()
	cp := Library{Tracks: append([]*Track(nil), lib.Tracks...), Scanned: lib.Scanned}
	libMu.RUnlock()
	saveJSON("library.json", cp)
}

func getTrack(id string) *Track {
	libMu.RLock()
	defer libMu.RUnlock()
	return byID[id]
}

func tracksByKey() map[string][]*Track {
	libMu.RLock()
	defer libMu.RUnlock()
	m := map[string][]*Track{}
	for _, t := range lib.Tracks {
		m[t.Key] = append(m[t.Key], t)
	}
	return m
}

// ---------- scanning ----------

type ScanStatus struct {
	Running bool   `json:"running"`
	Found   int    `json:"found"`
	Done    int    `json:"done"`
	Current string `json:"current"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
	Seq     int64  `json:"seq"`
	USB     string `json:"usb,omitempty"` // note about a merged 拾音数据.json (USB copy from the phone)
}

var (
	scanMu     sync.Mutex
	scanStatus ScanStatus
)

func getScanStatus() ScanStatus {
	scanMu.Lock()
	defer scanMu.Unlock()
	return scanStatus
}

func startScan() bool {
	scanMu.Lock()
	if scanStatus.Running {
		scanMu.Unlock()
		return false
	}
	scanStatus = ScanStatus{Running: true, Seq: scanStatus.Seq}
	scanMu.Unlock()
	go runScan()
	return true
}

func runScan() {
	libMu.RLock()
	old := map[string]*Track{}
	for _, t := range lib.Tracks {
		old[t.Path] = t
	}
	libMu.RUnlock()

	var files, dataFiles []string
	seen := map[string]bool{}
	for _, dir := range libraryDirs() {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() == usbDataFile {
				dataFiles = append(dataFiles, p)
				return nil
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(p), "."))
			if !audioExts[ext] {
				return nil
			}
			key := strings.ToLower(p)
			if !seen[key] {
				seen[key] = true
				files = append(files, p)
				scanMu.Lock()
				scanStatus.Found = len(files)
				scanMu.Unlock()
			}
			return nil
		})
	}
	scanMu.Lock()
	scanStatus.Total = len(files)
	scanMu.Unlock()

	out := make([]*Track, 0, len(files))
	dc := map[string]string{}
	for i, p := range files {
		st, err := os.Stat(p)
		if err != nil || st.Size() < 1024 {
			continue
		}
		var t *Track
		if o := old[p]; o != nil && o.Size == st.Size() && o.Mtime == st.ModTime().Unix() {
			t = o
		} else {
			t = readTrack(p, st)
		}
		t.DirCover = ""
		if !t.EmbCover {
			t.DirCover = findDirCover(p, dc)
		}
		out = append(out, t)
		if i%10 == 0 {
			scanMu.Lock()
			scanStatus.Done = i + 1
			scanStatus.Current = filepath.Base(p)
			scanMu.Unlock()
		}
	}
	sortTracks(out)
	libMu.Lock()
	lib.Tracks = out
	lib.Scanned = time.Now().Unix()
	reindex()
	libMu.Unlock()
	saveLibrary()
	updateConfig(func(c *Config) { c.Scanned = true })
	usb := mergeUSBData(dataFiles) // phone data copied by USB cable (playlists, names, covers)
	scanMu.Lock()
	scanStatus.USB = usb
	scanStatus.Running = false
	scanStatus.Done = len(files)
	scanStatus.Current = ""
	scanStatus.Seq++
	scanMu.Unlock()
}

func sortTracks(ts []*Track) {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].Added > ts[j].Added })
}

// addFiles adds (or refreshes) single files without a full rescan, e.g. after an import.
func addFiles(paths []string) {
	var add []*Track
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		t := readTrack(p, st)
		if !t.EmbCover {
			t.DirCover = findDirCover(p, nil)
		}
		add = append(add, t)
	}
	if len(add) == 0 {
		return
	}
	libMu.Lock()
	idx := map[string]int{}
	for i, t := range lib.Tracks {
		idx[t.ID] = i
	}
	for _, t := range add {
		if i, ok := idx[t.ID]; ok {
			lib.Tracks[i] = t
		} else {
			lib.Tracks = append(lib.Tracks, t)
		}
	}
	sortTracks(lib.Tracks)
	reindex()
	libMu.Unlock()
	saveLibrary()
}

func trackID(path string) string { return hashStr(strings.ToLower(filepath.Clean(path))) }

func readTrack(p string, st os.FileInfo) *Track {
	name := filepath.Base(p)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	t := &Track{
		ID: trackID(p), Path: p, FileName: name, Ext: ext, Size: st.Size(),
		Mtime: st.ModTime().Unix(), Added: st.ModTime().Unix(),
		Key: trackKey(name, st.Size()),
	}
	if f, err := os.Open(p); err == nil {
		func() {
			defer func() { recover() }()
			m, err := tag.ReadFrom(f)
			if err == nil && m != nil {
				t.Title = fixEnc(m.Title())
				t.Artist = fixEnc(m.Artist())
				if t.Artist == "" {
					t.Artist = fixEnc(m.AlbumArtist())
				}
				t.Album = fixEnc(m.Album())
				t.TrackNo, _ = m.Track()
				if pic := m.Picture(); pic != nil && len(pic.Data) > 100 {
					t.EmbCover = true
				}
				if strings.TrimSpace(lyricsOf(m)) != "" {
					t.EmbLyr = true
				}
			}
		}()
		f.Close()
	}
	if t.Title == "" {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		base = strings.TrimSpace(dupSuffix.ReplaceAllString(base, ""))
		if a, b, ok := splitArtistTitle(base); ok {
			t.Title = b
			if t.Artist == "" {
				t.Artist = a
			}
		} else {
			t.Title = base
		}
	}
	t.Duration = probeDuration(p)
	return t
}

func splitArtistTitle(s string) (string, string, bool) {
	for _, sep := range []string{" - ", " – ", " — ", "－"} {
		if i := strings.Index(s, sep); i > 0 {
			a := strings.TrimSpace(s[:i])
			b := strings.TrimSpace(s[i+len(sep):])
			if a != "" && b != "" {
				return a, b, true
			}
		}
	}
	return "", "", false
}

// fixEnc repairs the classic Chinese ID3 problem: GBK bytes stored as ISO-8859-1.
func fixEnc(s string) string {
	s = strings.TrimSpace(strings.TrimRight(s, "\x00"))
	if s == "" {
		return s
	}
	high := false
	for _, r := range s {
		if r > 0xff {
			return s
		}
		if r >= 0x80 {
			high = true
		}
	}
	if !high {
		return s
	}
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if d, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil && utf8.Valid(d) {
		return string(d)
	}
	return s
}

// ---------- display (tags < phone metadata < user edits) ----------

type TrackView struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	RawTitle  string `json:"rawTitle"`
	RawArtist string `json:"rawArtist"`
	RawAlbum  string `json:"rawAlbum"`
	Duration  int64  `json:"duration"`
	Size      int64  `json:"size"`
	FileName  string `json:"fileName"`
	Path      string `json:"path"`
	Ext       string `json:"ext"`
	TrackNo   int    `json:"trackNo,omitempty"`
	Added     int64  `json:"added"`
	Cover     bool   `json:"cover"`
	CoverV    int64  `json:"cv"`
	Edited    bool   `json:"edited,omitempty"` // has a display-name override (user edit, 自动识别 or phone)
	Auto      bool   `json:"auto,omitempty"`   // the override came from 识别中文歌名
	Tried     bool   `json:"tried,omitempty"`  // 识别中文歌名 found nothing for it
	CustomCov bool   `json:"customCover,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
}

func viewOf(t *Track) TrackView {
	v := TrackView{ID: t.ID, Key: t.Key, Title: t.Title, Artist: t.Artist, Album: t.Album,
		RawTitle: t.Title, RawArtist: t.Artist, RawAlbum: t.Album, Duration: t.Duration, Size: t.Size,
		FileName: t.FileName, Path: t.Path, Ext: t.Ext, TrackNo: t.TrackNo, Added: t.Added, Cover: t.EmbCover || t.DirCover != ""}
	metaMu.Lock()
	pm, hasP := meta.Phone[t.Key]
	um, hasU := meta.User[t.Key]
	metaMu.Unlock()
	apply := func(m Meta) {
		if m.Title != "" {
			v.Title = m.Title
		}
		if m.Artist != "" {
			v.Artist = m.Artist
		}
		if m.Album != "" {
			v.Album = m.Album
		}
		if m.Cover != "" {
			v.Cover = true
			v.CoverV = m.CoverV
		}
	}
	if hasP {
		apply(pm)
	}
	if hasU {
		apply(um)
		v.Edited = um.named()
		v.Auto = um.named() && um.Src == "online"
		v.Tried = um.Tried > 0
		v.CustomCov = um.Cover != ""
	}
	if v.Artist == "" {
		v.Artist = "未知歌手"
	}
	if v.Album == "" {
		v.Album = "未知专辑"
	}
	return v
}

// coverFileFor returns the stored cover file for a key (user edit first, then the phone's), or "".
func coverFileFor(key string) string {
	metaMu.Lock()
	defer metaMu.Unlock()
	if m, ok := meta.User[key]; ok && m.Cover != "" {
		return filepath.Join(dataDir, "covers", m.Cover)
	}
	if m, ok := meta.Phone[key]; ok && m.Cover != "" {
		return filepath.Join(dataDir, "covers", m.Cover)
	}
	return ""
}

func embeddedCover(path string) ([]byte, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	defer func() { recover() }()
	m, err := tag.ReadFrom(f)
	if err != nil || m == nil {
		return nil, ""
	}
	p := m.Picture()
	if p == nil {
		return nil, ""
	}
	return p.Data, p.MIMEType
}

func embeddedLyrics(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	defer func() { recover() }()
	m, err := tag.ReadFrom(f)
	if err != nil || m == nil {
		return ""
	}
	return fixEnc(lyricsOf(m))
}

// lyricsOf: USLT / ©lyr / LYRICS, plus TXXX or vorbis fields whose name mentions lyrics.
func lyricsOf(m tag.Metadata) string {
	if l := m.Lyrics(); strings.TrimSpace(l) != "" {
		return l
	}
	for k, v := range m.Raw() {
		lk := strings.ToLower(k)
		switch x := v.(type) {
		case *tag.Comm:
			d := strings.ToLower(x.Description)
			if strings.Contains(d, "lyric") || d == "uslt" {
				return x.Text
			}
		case string:
			if strings.Contains(lk, "lyric") {
				return x
			}
		}
	}
	return ""
}

// waitScan blocks until the running scan finishes.
func waitScan() {
	for getScanStatus().Running {
		time.Sleep(200 * time.Millisecond)
	}
}
