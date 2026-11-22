package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type LyricResult struct {
	Source string `json:"source"` // file | embedded | cache | qq | kugou | netease | lrclib | none
	Text   string `json:"text"`
	Synced bool   `json:"synced"`
}

var syncedRe = regexp.MustCompile(`\[\d{1,3}:\d{1,2}([.:]\d{1,3})?]`)

func isSynced(s string) bool { return syncedRe.MatchString(s) }

func decodeText(b []byte) string {
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		return string(b[3:])
	}
	if len(b) >= 2 && ((b[0] == 0xff && b[1] == 0xfe) || (b[0] == 0xfe && b[1] == 0xff)) {
		le := b[0] == 0xff
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if le {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			}
		}
		return string(utf16.Decode(u))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if d, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(d)
	}
	return string(b)
}

func sidecarLrc(path string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range []string{".lrc", ".LRC", ".Lrc"} {
		if b, err := os.ReadFile(base + ext); err == nil && len(b) > 0 {
			return decodeText(b)
		}
	}
	return ""
}

func lyricCachePath(key string) string {
	return filepath.Join(dataDir, "lyrics", hashStr(key)+".lrc")
}

var (
	missMu sync.Mutex
	missed = map[string]time.Time{} // online misses, not retried for a while
)

func getLyrics(t *Track, online bool, force bool) LyricResult {
	if b, err := os.ReadFile(manualLyricPath(t.Key)); err == nil && len(b) > 0 {
		s := string(b)
		return LyricResult{"manual", s, isSynced(s)}
	}
	if !force {
		if s := sidecarLrc(t.Path); strings.TrimSpace(s) != "" {
			return LyricResult{"file", s, isSynced(s)}
		}
		if t.EmbLyr {
			if s := embeddedLyrics(t.Path); strings.TrimSpace(s) != "" {
				return LyricResult{"embedded", s, isSynced(s)}
			}
		}
		if b, err := os.ReadFile(lyricCachePath(t.Key)); err == nil && len(b) > 0 {
			s := string(b)
			return LyricResult{"cache", s, isSynced(s)}
		}
	}
	if !online || !getConfig().OnlineLyrics {
		return LyricResult{Source: "none"}
	}
	missMu.Lock()
	if tm, ok := missed[t.Key]; ok && !force && time.Since(tm) < 30*time.Minute {
		missMu.Unlock()
		return LyricResult{Source: "none"}
	}
	missMu.Unlock()
	v := viewOf(t)
	r := fetchOnline(v.Title, v.Artist, t.Title, t.Artist, t.Duration)
	if r == nil {
		missMu.Lock()
		missed[t.Key] = time.Now()
		missMu.Unlock()
		return LyricResult{Source: "none"}
	}
	os.WriteFile(lyricCachePath(t.Key), []byte(r.Text), 0o644)
	return *r
}

// ---------- online search: QQ音乐 → 酷狗 → 网易云 → LRCLIB, synced preferred ----------

const webUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

var onlineClient = &http.Client{Timeout: 12 * time.Second, Transport: webTransport}

func httpGet(u string, headers map[string]string) (int, []byte) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return 0, nil
	}
	req.Header.Set("User-Agent", webUA)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := onlineClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b
}

type cand struct {
	src, id, title, artist, album string
	durMs                         int64
	extra                         string
}

var bracketRe = regexp.MustCompile(`[(（\[【].*?[)）\]】]`)
var punctRe = regexp.MustCompile(`[\s\p{P}\p{S}]`)
var liveRe = regexp.MustCompile(`(?i)\s*-\s*(live|remaster(ed)?.*)$`)

func norm(s string) string {
	return punctRe.ReplaceAllString(bracketRe.ReplaceAllString(strings.ToLower(s), ""), "")
}

func sameTitle(a, b string) bool {
	x, y := norm(a), norm(b)
	if x == "" || y == "" {
		return false
	}
	return x == y || (utf8.RuneCountInString(x) >= 2 && strings.Contains(y, x)) || (utf8.RuneCountInString(y) >= 2 && strings.Contains(x, y))
}

func durOk(c, t, tol int64) bool {
	if t <= 0 || c <= 0 {
		return true
	}
	d := c - t
	if d < 0 {
		d = -d
	}
	return d <= tol
}

var entRep = strings.NewReplacer("&apos;", "'", "&quot;", "\"", "&#39;", "'", "&#58;", ":", "&#40;", "(", "&#41;", ")",
	"&#45;", "-", "&#32;", " ", "&#10;", "\n", "&#13;", "", "&lt;", "<", "&gt;", ">", "&amp;", "&")

type tq struct{ title, artist string }

func queries(dTitle, dArtist, rTitle, rArtist string) []tq {
	var out []tq
	seen := map[tq]bool{}
	add := func(q tq) {
		if !seen[q] && q.title != "" {
			seen[q] = true
			out = append(out, q)
		}
	}
	for _, p := range []tq{{dTitle, dArtist}, {rTitle, rArtist}} {
		title := strings.TrimSpace(p.title)
		artist := p.artist
		if artist == "未知歌手" {
			artist = ""
			if a, b, ok := splitArtistTitle(title); ok {
				add(tq{b, a})
				add(tq{a, b})
			}
		}
		add(tq{title, artist})
		stripped := strings.TrimSpace(liveRe.ReplaceAllString(bracketRe.ReplaceAllString(title, ""), ""))
		first := artist
		for _, sep := range []string{"/", "、", ",", "，", "&", ";", " feat", " ft."} {
			if i := strings.Index(first, sep); i >= 0 {
				first = first[:i]
			}
		}
		first = strings.TrimSpace(first)
		if stripped != "" && (stripped != title || first != artist) {
			add(tq{stripped, first})
		}
	}
	return out
}

func qqSearch(q string) []cand {
	code, b := httpGet("https://c.y.qq.com/soso/fcgi-bin/client_search_cp?w="+url.QueryEscape(q)+"&format=json&p=1&n=8&cr=1&new_json=0",
		map[string]string{"Referer": "https://y.qq.com/"})
	if code != 200 {
		return nil
	}
	b = bytes.TrimSpace(b)
	b = bytes.TrimSuffix(bytes.TrimPrefix(b, []byte("callback(")), []byte(")"))
	var r struct {
		Data struct {
			Song struct {
				List []struct {
					Songmid, Songname, Albumname string
					Interval                     int64
					Singer                       []struct{ Name string }
				}
			}
		}
	}
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	var out []cand
	for _, s := range r.Data.Song.List {
		var ar []string
		for _, x := range s.Singer {
			ar = append(ar, x.Name)
		}
		if s.Songmid != "" {
			out = append(out, cand{"qq", s.Songmid, entRep.Replace(s.Songname), entRep.Replace(strings.Join(ar, "/")), entRep.Replace(s.Albumname), s.Interval * 1000, ""})
		}
	}
	return out
}

func qqLyric(mid string) string {
	code, b := httpGet("https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg?songmid="+url.QueryEscape(mid)+"&format=json&nobase64=1&g_tk=5381",
		map[string]string{"Referer": "https://y.qq.com/portal/player.html"})
	if code != 200 {
		return ""
	}
	b = bytes.TrimSpace(b)
	b = bytes.TrimSuffix(bytes.TrimPrefix(b, []byte("MusicJsonCallback(")), []byte(")"))
	var r struct{ Lyric string }
	json.Unmarshal(b, &r)
	return strings.TrimSpace(entRep.Replace(r.Lyric))
}

func kugouSearch(kw string, durMs int64) []cand {
	d := ""
	if durMs > 0 {
		d = fmt.Sprint(durMs)
	}
	code, b := httpGet("https://lyrics.kugou.com/search?ver=1&man=yes&client=pc&keyword="+url.QueryEscape(kw)+"&duration="+d+"&hash=", nil)
	if code != 200 {
		return nil
	}
	var r struct {
		Candidates []struct {
			ID        json.Number `json:"id"`
			Accesskey string
			Song      string
			Singer    string
			Duration  int64
		}
	}
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	var out []cand
	for _, c := range r.Candidates {
		if c.ID != "" {
			out = append(out, cand{"kugou", c.ID.String(), c.Song, c.Singer, "", c.Duration, c.Accesskey})
		}
	}
	return out
}

func kugouLyric(c cand) string {
	code, b := httpGet("https://lyrics.kugou.com/download?ver=1&client=pc&id="+url.QueryEscape(c.id)+"&accesskey="+url.QueryEscape(c.extra)+"&fmt=lrc&charset=utf8", nil)
	if code != 200 {
		return ""
	}
	var r struct{ Content string }
	json.Unmarshal(b, &r)
	if r.Content == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(r.Content)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(decodeText(raw))
}

func neteaseSearch(q string) []cand {
	code, b := httpGet("https://music.163.com/api/cloudsearch/pc?s="+url.QueryEscape(q)+"&type=1&limit=8&offset=0",
		map[string]string{"Referer": "https://music.163.com/"})
	if code != 200 {
		return nil
	}
	var r struct {
		Result struct {
			Songs []struct {
				ID   int64
				Name string
				Dt   int64
				Ar   []struct{ Name string }
				Al   struct{ Name string }
			}
		}
	}
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	var out []cand
	for _, s := range r.Result.Songs {
		var ar []string
		for _, x := range s.Ar {
			ar = append(ar, x.Name)
		}
		out = append(out, cand{"netease", fmt.Sprint(s.ID), s.Name, strings.Join(ar, "/"), s.Al.Name, s.Dt, ""})
	}
	return out
}

func neteaseLyric(id string) string {
	code, b := httpGet("https://music.163.com/api/song/lyric?id="+url.QueryEscape(id)+"&lv=1&tv=-1", map[string]string{"Referer": "https://music.163.com/"})
	if code != 200 {
		return ""
	}
	var r struct{ Lrc struct{ Lyric string } }
	json.Unmarshal(b, &r)
	return strings.TrimSpace(r.Lrc.Lyric)
}

func lyricOf(c cand) string {
	defer func() { recover() }()
	switch c.src {
	case "qq":
		return qqLyric(c.id)
	case "kugou":
		return kugouLyric(c)
	case "netease":
		return neteaseLyric(c.id)
	}
	return ""
}

type lrclibItem struct {
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  *string `json:"plainLyrics"`
	SyncedLyrics *string `json:"syncedLyrics"`
}

func fromLrclib(o lrclibItem) *LyricResult {
	if o.SyncedLyrics != nil && strings.TrimSpace(*o.SyncedLyrics) != "" {
		return &LyricResult{"lrclib", *o.SyncedLyrics, true}
	}
	if o.PlainLyrics != nil && strings.TrimSpace(*o.PlainLyrics) != "" {
		return &LyricResult{"lrclib", *o.PlainLyrics, false}
	}
	if o.Instrumental {
		return &LyricResult{"lrclib", "[00:00.00]纯音乐，请欣赏", true}
	}
	return nil
}

func lrclib(title, artist string, durMs int64) *LyricResult {
	h := map[string]string{"Lrclib-Client": "Shiyin-PC/" + appVersion}
	durS := float64(durMs) / 1000
	if artist != "" {
		u := lrclibBase() + "/api/get?track_name=" + url.QueryEscape(title) + "&artist_name=" + url.QueryEscape(artist)
		if durS > 0 {
			u += fmt.Sprintf("&duration=%d", int(math.Round(durS)))
		}
		if code, b := httpGet(u, h); code == 200 {
			var o lrclibItem
			if json.Unmarshal(b, &o) == nil {
				if r := fromLrclib(o); r != nil && r.Synced {
					return r
				}
			}
		}
	}
	u := lrclibBase() + "/api/search?track_name=" + url.QueryEscape(title)
	if artist != "" {
		u += "&artist_name=" + url.QueryEscape(artist)
	}
	code, b := httpGet(u, h)
	if code != 200 {
		return nil
	}
	var arr []lrclibItem
	if json.Unmarshal(b, &arr) != nil {
		return nil
	}
	var best *LyricResult
	bestScore := math.MaxFloat64
	for _, o := range arr {
		r := fromLrclib(o)
		if r == nil {
			continue
		}
		diff := 5.0
		if durS > 0 && o.Duration > 0 {
			diff = math.Abs(o.Duration - durS)
			if diff > 10 {
				continue
			}
		}
		score := diff
		if !r.Synced {
			score += 6
		}
		if score < bestScore {
			bestScore, best = score, r
		}
	}
	return best
}

func fetchOnline(dTitle, dArtist, rTitle, rArtist string, durMs int64) *LyricResult {
	var plain *LyricResult
	tried := map[string]bool{}
	consider := func(c cand) *LyricResult {
		if tried[c.src+c.id] {
			return nil
		}
		tried[c.src+c.id] = true
		text := lyricOf(c)
		if text == "" {
			return nil
		}
		r := &LyricResult{c.src, text, isSynced(text)}
		if r.Synced {
			return r
		}
		if plain == nil {
			plain = r
		}
		return nil
	}
	take := func(cs []cand, ok func(cand) bool) []cand {
		var out []cand
		for _, c := range cs {
			if ok(c) {
				out = append(out, c)
				if len(out) == 2 {
					break
				}
			}
		}
		return out
	}
	for _, q := range queries(dTitle, dArtist, rTitle, rArtist) {
		if cnLyricsOff() {
			if r := lrclib(q.title, q.artist, durMs); r != nil {
				return r
			}
			continue
		}
		kw := strings.TrimSpace(q.title + " " + q.artist)
		for _, c := range take(qqSearch(kw), func(c cand) bool { return sameTitle(c.title, q.title) && durOk(c.durMs, durMs, 3500) }) {
			if r := consider(c); r != nil {
				return r
			}
		}
		kk := q.title
		if q.artist != "" {
			kk = q.artist + " - " + q.title
		}
		for _, c := range take(kugouSearch(kk, durMs), func(c cand) bool { return durOk(c.durMs, durMs, 3000) }) {
			if r := consider(c); r != nil {
				return r
			}
		}
		for _, c := range take(neteaseSearch(kw), func(c cand) bool { return sameTitle(c.title, q.title) && durOk(c.durMs, durMs, 3500) }) {
			if r := consider(c); r != nil {
				return r
			}
		}
		if r := lrclib(q.title, q.artist, durMs); r != nil {
			if r.Synced {
				return r
			}
			if plain == nil {
				plain = r
			}
		}
	}
	return plain
}

func lrclibBase() string {
	if u := os.Getenv("SHIYIN_LRCLIB_URL"); u != "" { // tests only
		return strings.TrimRight(u, "/")
	}
	return "https://lrclib.net"
}

// tests only: skip QQ音乐 / 酷狗 / 网易云 so results are deterministic
func cnLyricsOff() bool { return os.Getenv("SHIYIN_NO_CN_LYRICS") != "" }

func manualLyricPath(key string) string {
	return filepath.Join(dataDir, "lyrics", hashStr(key)+".manual.lrc")
}

func saveManualLyrics(t *Track, text string) error {
	return os.WriteFile(manualLyricPath(t.Key), []byte(text), 0o644)
}

// resetLyrics forgets the manual choice and the cached online result, so automatic matching runs again.
func resetLyrics(t *Track) {
	os.Remove(manualLyricPath(t.Key))
	os.Remove(lyricCachePath(t.Key))
	missMu.Lock()
	delete(missed, t.Key)
	missMu.Unlock()
}

type LyricItem struct {
	Src      string  `json:"src"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Duration float64 `json:"duration"` // seconds
	Synced   bool    `json:"synced"`
	Text     string  `json:"text"`
}

// searchLyrics queries all four sources in parallel (manual 「搜索歌词」 sheet, like the phone's Lyrics.search).
func searchLyrics(title, artist string) ([]LyricItem, error) {
	q := strings.TrimSpace(title + " " + artist)
	type res struct {
		items []LyricItem
		ok    bool
	}
	var fns []func() []LyricItem
	fromCands := func(src string, cs []cand) []LyricItem {
		if len(cs) > 5 {
			cs = cs[:5]
		}
		out := make([]LyricItem, len(cs))
		var wg sync.WaitGroup
		for i, c := range cs {
			wg.Add(1)
			go func(i int, c cand) {
				defer wg.Done()
				if text := lyricOf(c); text != "" {
					out[i] = LyricItem{src, c.title, c.artist, c.album, float64(c.durMs) / 1000, isSynced(text), text}
				}
			}(i, c)
		}
		wg.Wait()
		var keep []LyricItem
		for _, it := range out {
			if it.Text != "" {
				keep = append(keep, it)
			}
		}
		return keep
	}
	if !cnLyricsOff() {
		fns = append(fns,
			func() []LyricItem { return fromCands("QQ音乐", qqSearch(q)) },
			func() []LyricItem {
				kk := title
				if artist != "" {
					kk = artist + " - " + title
				}
				return fromCands("酷狗", kugouSearch(kk, 0))
			},
			func() []LyricItem { return fromCands("网易云", neteaseSearch(q)) })
	}
	fns = append(fns, func() []LyricItem {
		u := lrclibBase() + "/api/search?q=" + url.QueryEscape(title)
		if artist != "" {
			u = lrclibBase() + "/api/search?track_name=" + url.QueryEscape(title) + "&artist_name=" + url.QueryEscape(artist)
		}
		code, b := httpGet(u, map[string]string{"Lrclib-Client": "Shiyin-PC/" + appVersion})
		if code != 200 {
			return nil
		}
		var arr []struct {
			TrackName, ArtistName, AlbumName string
			Duration                         float64
			PlainLyrics, SyncedLyrics        *string
		}
		if json.Unmarshal(b, &arr) != nil {
			return nil
		}
		var out []LyricItem
		for i, o := range arr {
			if i >= 15 {
				break
			}
			text := ""
			if o.SyncedLyrics != nil && strings.TrimSpace(*o.SyncedLyrics) != "" {
				text = *o.SyncedLyrics
			} else if o.PlainLyrics != nil {
				text = *o.PlainLyrics
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, LyricItem{"LRCLIB", o.TrackName, o.ArtistName, o.AlbumName, o.Duration, isSynced(text), text})
		}
		return out
	})
	ch := make(chan []LyricItem, len(fns))
	for _, f := range fns {
		go func(f func() []LyricItem) {
			defer func() {
				if recover() != nil {
					ch <- nil
				}
			}()
			ch <- f()
		}(f)
	}
	var all []LyricItem
	timeout := time.After(25 * time.Second)
	for range fns {
		select {
		case items := <-ch:
			all = append(all, items...)
		case <-timeout:
			return all, nil
		}
	}
	return all, nil
}
