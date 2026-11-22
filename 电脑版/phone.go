package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ================= discovery =================

const discoverPort = 18521

type PhoneInfo struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	Version string `json:"version"`
	Count   int    `json:"count"`
}

func broadcastAddrs() []string {
	out := []string{"255.255.255.255"}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || len(ipn.Mask) != 4 {
				continue
			}
			b := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				b[i] = ip4[i] | ^ipn.Mask[i]
			}
			s := b.String()
			if s != ip4.String() {
				out = append(out, s)
			}
		}
	}
	if extra := os.Getenv("SHIYIN_DISCOVER_EXTRA"); extra != "" { // testing hook
		out = append(out, strings.Split(extra, ",")...)
	}
	return out
}

func discoverPhones(wait time.Duration) []PhoneInfo {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()
	msg := []byte("SHIYIN_DISCOVER")
	targets := broadcastAddrs()
	send := func() {
		for _, t := range targets {
			conn.WriteToUDP(msg, &net.UDPAddr{IP: net.ParseIP(t), Port: discoverPort})
		}
	}
	send()
	resent := false
	deadline := time.Now().Add(wait)
	conn.SetReadDeadline(deadline)
	seen := map[string]bool{}
	var out []PhoneInfo
	buf := make([]byte, 4096)
	for {
		if !resent && time.Until(deadline) < wait/2 {
			send() // UDP is lossy; send once more halfway through
			resent = true
		}
		conn.SetReadDeadline(minTime(deadline, time.Now().Add(wait/2)))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if time.Now().Before(deadline) {
				continue
			}
			break
		}
		var p PhoneInfo
		if json.Unmarshal(buf[:n], &p) != nil || p.App != "shiyin" {
			continue
		}
		if p.IP == "" {
			p.IP = from.IP.String()
		}
		if p.Port == 0 {
			p.Port = 18520
		}
		id := fmt.Sprintf("%s:%d", p.IP, p.Port)
		if !seen[id] {
			seen[id] = true
			out = append(out, p)
		}
	}
	return out
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// ================= session =================

type PhoneTrack struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	RawTitle  string `json:"rawTitle"`
	RawArtist string `json:"rawArtist"`
	Duration  int64  `json:"duration"`
	Size      int64  `json:"size"`
	FileName  string `json:"fileName"`
	Ext       string `json:"ext"`
	TrackNo   int    `json:"trackNo"`
	DateAdded int64  `json:"dateAdded"`
	HasLrc    bool   `json:"hasLrc"`
	LrcSynced bool   `json:"lrcSynced"`
	HasCover  bool   `json:"hasCover"`
	CoverVer  int64  `json:"coverVer"`
	Present   bool   `json:"present"` // computed on the PC
}

type PhonePlaylist struct {
	Name string   `json:"name"`
	Fav  bool     `json:"fav"`
	Keys []string `json:"keys"`
}

type PhoneLibrary struct {
	App       string          `json:"app"`
	Version   string          `json:"version"`
	Name      string          `json:"name"`
	Tracks    []PhoneTrack    `json:"tracks"`
	Playlists []PhonePlaylist `json:"playlists"`
}

type phoneSession struct {
	base    string // http://ip:port (reverse: a placeholder host, the dialer ignores it)
	code    string
	lib     PhoneLibrary
	client  *http.Client // nil = dlClient (direct LAN)
	reverse bool         // 「手机连电脑」: requests go over connections the phone opened
}

func (s *phoneSession) http() *http.Client {
	if s.client != nil {
		return s.client
	}
	return dlClient
}

var (
	phMu sync.Mutex
	ph   *phoneSession
)

func (s *phoneSession) apiURL(path string, q url.Values) string {
	if q == nil {
		q = url.Values{}
	}
	q.Set("code", s.code)
	return s.base + path + "?" + q.Encode()
}

func (s *phoneSession) newReq(ctx context.Context, path string, q url.Values) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.apiURL(path, q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shiyin-Code", s.code)
	req.Header.Set("User-Agent", "Shiyin-PC/"+appVersion)
	// 0.2.2: tell the phone who we are on its LAN, so it shows 「已连接电脑 <name> (192.168.x.y)」 even if the
	// connection arrives from another address (phone VPN / NAT).
	if host, err := os.Hostname(); err == nil {
		req.Header.Set("X-Shiyin-Name", url.QueryEscape(host))
	}
	if u, err := url.Parse(s.base); err == nil && !s.reverse {
		if ip := net.ParseIP(u.Hostname()); ip != nil {
			if lip := localIPFor(ip); lip != nil {
				req.Header.Set("X-Shiyin-From", lip.String())
			}
		}
	}
	return req, nil
}

func normalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
	host = strings.TrimSuffix(host, "/")
	host = strings.ReplaceAll(host, "：", ":")
	if host == "" {
		return "", errors.New("请输入手机上显示的 IP:端口")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = host + ":18520"
	}
	h, p, err := net.SplitHostPort(host)
	if err != nil || h == "" {
		return "", errors.New("地址格式不对，应为 192.168.1.23:18520 这样")
	}
	if _, err := strconv.Atoi(p); err != nil {
		return "", errors.New("端口不对")
	}
	return net.JoinHostPort(h, p), nil
}

// connectPhone pings the phone, then fetches its library (can take ~30 s the first time on the phone).
func connectPhone(host, code string) (*PhoneLibrary, error) {
	hp, err := normalizeHost(host)
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return nil, errors.New("配对码是手机上显示的 6 位数字")
	}
	s := &phoneSession{base: "http://" + hp, code: code}

	pc := &http.Client{Timeout: 6 * time.Second, Transport: lanTransport}
	resp, err := pc.Get(s.base + "/api/ping")
	if err != nil {
		return nil, fmt.Errorf("连不上手机（%s）。请确认手机已打开「传到电脑」，并且和电脑在同一个 Wi-Fi；还是不行就用下面的「改用手机连电脑」", hp)
	}
	var ping struct{ App string }
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&ping)
	resp.Body.Close()
	if ping.App != "shiyin" {
		return nil, errors.New("这个地址不是拾音手机端")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, _ := s.newReq(ctx, "/api/library", nil)
	resp, err = dlClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("读取手机曲库超时（90 秒），请重试")
		}
		return nil, fmt.Errorf("读取手机曲库失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("配对码不对，请看手机上显示的 6 位配对码")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("手机返回错误 %d", resp.StatusCode)
	}
	var lib PhoneLibrary
	if err := json.NewDecoder(resp.Body).Decode(&lib); err != nil {
		return nil, fmt.Errorf("手机曲库格式不对：%v", err)
	}
	s.lib = lib
	phMu.Lock()
	ph = s
	phMu.Unlock()
	updateConfig(func(c *Config) { c.LastPhone = hp })
	markPresent(&s.lib)
	return &s.lib, nil
}

// markPresent flags phone tracks the PC already has: same key in the library,
// or a same-name same-size file already in the music folder.
func markPresent(l *PhoneLibrary) {
	byKey := tracksByKey()
	dir := getConfig().MusicDir
	for i := range l.Tracks {
		t := &l.Tracks[i]
		key := t.Key
		if key == "" {
			key = trackKey(t.FileName, t.Size)
			t.Key = key
		}
		pk := trackKey(t.FileName, t.Size)
		t.Present = len(byKey[key]) > 0 || len(byKey[pk]) > 0
		if !t.Present && t.FileName != "" {
			if st, err := os.Stat(filepath.Join(dir, safeName(t.FileName))); err == nil && st.Size() == t.Size {
				t.Present = true
			}
		}
	}
}

func refreshPresent() *PhoneLibrary {
	phMu.Lock()
	defer phMu.Unlock()
	if ph == nil {
		return nil
	}
	markPresent(&ph.lib)
	cp := ph.lib
	return &cp
}

var badChars = strings.NewReplacer("<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_", "|", "_", "?", "_", "*", "_")

func safeName(n string) string {
	n = strings.TrimSpace(badChars.Replace(n))
	n = strings.TrimRight(n, ". ")
	if n == "" {
		n = "未命名"
	}
	return n
}

// ================= import job =================

type ImportItem struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	State  string `json:"state"` // wait | down | done | skip | fail
	Got    int64  `json:"got"`
	Size   int64  `json:"size"`
	Error  string `json:"error,omitempty"`
	Try    int    `json:"try,omitempty"`
}

type ImportJob struct {
	Running   bool          `json:"running"`
	Cancelled bool          `json:"cancelled"`
	Items     []*ImportItem `json:"items"`
	Done      int           `json:"done"`
	Failed    int           `json:"failed"`
	Skipped   int           `json:"skipped"`
	Total     int           `json:"total"`
	Bytes     int64         `json:"bytes"`
	TotalB    int64         `json:"totalBytes"`
	Message   string        `json:"message"`
	Started   int64         `json:"started"`
	Finished  int64         `json:"finished"`
	Seq       int64         `json:"seq"`
	Slow      bool          `json:"slow"` // 0.2.3: over the PC hotspot → 1 at a time, ~6 MB/s
}

var (
	jobMu     sync.Mutex
	job       = &ImportJob{}
	jobCancel context.CancelFunc
)

func importStatus() ImportJob {
	jobMu.Lock()
	defer jobMu.Unlock()
	cp := *job
	cp.Items = make([]*ImportItem, len(job.Items))
	for i, it := range job.Items {
		c := *it
		cp.Items[i] = &c
	}
	var b int64
	for _, it := range job.Items {
		if it.State == "done" || it.State == "skip" {
			b += it.Size
		} else {
			b += it.Got
		}
	}
	cp.Bytes = b
	return cp
}

func cancelImport() {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job.Running && jobCancel != nil {
		job.Cancelled = true
		jobCancel()
	}
}

func startImport(keys []string) error {
	phMu.Lock()
	s := ph
	phMu.Unlock()
	if s == nil {
		return errors.New("还没有连接手机")
	}
	jobMu.Lock()
	if job.Running {
		jobMu.Unlock()
		return errors.New("正在导入中")
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var list []PhoneTrack
	for _, t := range s.lib.Tracks {
		if want[t.Key] {
			list = append(list, t)
		}
	}
	if len(list) == 0 {
		jobMu.Unlock()
		return errors.New("没有选中歌曲")
	}
	ctx, cancel := context.WithCancel(context.Background())
	jobCancel = cancel
	seq := job.Seq + 1
	job = &ImportJob{Running: true, Total: len(list), Started: time.Now().Unix(), Seq: seq}
	for _, t := range list {
		job.Items = append(job.Items, &ImportItem{Key: t.Key, Title: t.Title, Artist: t.Artist, State: "wait", Size: t.Size})
		job.TotalB += t.Size
	}
	jobMu.Unlock()
	go runImport(ctx, s, list)
	return nil
}

func setItem(i int, f func(it *ImportItem)) {
	jobMu.Lock()
	f(job.Items[i])
	jobMu.Unlock()
}

func runImport(ctx context.Context, s *phoneSession, list []PhoneTrack) {
	dir := getConfig().MusicDir
	os.MkdirAll(dir, 0o755)
	byKey := tracksByKey()

	// decide target names up front (sequentially) so two downloads never pick the same name
	targets := make([]string, len(list))
	reserved := map[string]bool{}
	for i, t := range list {
		if len(byKey[t.Key]) > 0 {
			targets[i] = "" // already in the library
			continue
		}
		targets[i] = chooseTarget(dir, safeName(t.FileName), t.Size, reserved)
	}

	var newFiles []string
	var nfMu sync.Mutex
	// 0.2.3: over the PC's own hotspot (192.168.137.x) download one song at a time at ~6 MB/s:
	// some laptop Wi-Fi drivers (Intel AX201) froze the whole PC under sustained hotspot load.
	par := 2
	var lim *rateLimiter
	if viaPCHotspot(s.peerIP()) {
		par = 1
		lim = newRateLimiter(hotspotSlowRate)
		jobMu.Lock()
		job.Slow = true
		jobMu.Unlock()
	}
	sem := make(chan struct{}, par)
	var wg sync.WaitGroup
	for i := range list {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			t := list[i]
			target := targets[i]
			audioPath := ""
			skipped := false
			if target == "" {
				audioPath = byKey[t.Key][0].Path
				skipped = true
			} else if st, err := os.Stat(target); err == nil && st.Size() == t.Size {
				audioPath = target // same name, same size: already there (maybe not scanned yet)
				skipped = true
				nfMu.Lock()
				newFiles = append(newFiles, target)
				nfMu.Unlock()
			}
			if !skipped {
				setItem(i, func(it *ImportItem) { it.State = "down" })
				var err error
				for attempt := 0; attempt < 3; attempt++ { // first try + 2 retries
					if ctx.Err() != nil {
						break
					}
					setItem(i, func(it *ImportItem) { it.Try = attempt })
					err = downloadFile(ctx, s, t, target, lim, func(got int64) {
						setItem(i, func(it *ImportItem) { it.Got = got })
					})
					if err == nil {
						break
					}
					if attempt < 2 {
						select {
						case <-ctx.Done():
						case <-time.After(time.Duration(attempt+1) * 1500 * time.Millisecond):
						}
					}
				}
				if err != nil {
					msg := err.Error()
					if ctx.Err() != nil {
						msg = "已取消"
					}
					setItem(i, func(it *ImportItem) { it.State = "fail"; it.Error = msg })
					jobMu.Lock()
					job.Failed++
					jobMu.Unlock()
					return
				}
				audioPath = target
				nfMu.Lock()
				newFiles = append(newFiles, target)
				nfMu.Unlock()
			}
			// lyrics + cover + display metadata (best effort, never fails the song)
			syncExtras(ctx, s, t, audioPath)
			setItem(i, func(it *ImportItem) {
				if skipped {
					it.State = "skip"
				} else {
					it.State = "done"
				}
				it.Got = it.Size
			})
			jobMu.Lock()
			if skipped {
				job.Skipped++
			} else {
				job.Done++
			}
			jobMu.Unlock()
		}(i)
	}
	wg.Wait()
	saveMeta()
	addFiles(newFiles)
	n := importPlaylists(s.lib.Playlists)
	jobMu.Lock()
	job.Running = false
	job.Finished = time.Now().Unix()
	switch {
	case job.Cancelled:
		job.Message = fmt.Sprintf("已取消：导入 %d 首", job.Done)
	case job.Failed > 0:
		job.Message = fmt.Sprintf("完成：新导入 %d 首，已有 %d 首，失败 %d 首", job.Done, job.Skipped, job.Failed)
	default:
		job.Message = fmt.Sprintf("完成：新导入 %d 首，已有 %d 首", job.Done, job.Skipped)
	}
	if n > 0 {
		job.Message += fmt.Sprintf("，同步歌单 %d 个", n)
	}
	jobMu.Unlock()
	refreshPresent()
}

// chooseTarget: fileName, or "name (n).ext" when a different-size file already uses the name.
func chooseTarget(dir, name string, size int64, reserved map[string]bool) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 1; n < 1000; n++ {
		cand := name
		if n > 1 {
			cand = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		p := filepath.Join(dir, cand)
		lk := strings.ToLower(p)
		if reserved[lk] {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.Size() == size {
			reserved[lk] = true
			return p
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, time.Now().Unix(), ext))
}

// idleReader cancels the request when no bytes arrive for `idle`.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(ir.idle)
	}
	return n, err
}

// LAN connections to the phone never use a proxy (system / env / Clash) and are bound to the
// adapter on the phone's subnet, so a TUN "global" route can't swallow them.
var lanTransport = &http.Transport{
	Proxy:                 nil,
	DialContext:           lanDial,
	ResponseHeaderTimeout: 90 * time.Second,
	MaxIdleConnsPerHost:   4,
}

var dlClient = &http.Client{Transport: lanTransport}

func downloadFile(parent context.Context, s *phoneSession, t PhoneTrack, target string, lim *rateLimiter, progress func(int64)) error {
	part := target + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
		if t.Size > 0 && have > t.Size {
			os.Remove(part)
			have = 0
		}
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	const idle = 30 * time.Second
	timer := time.AfterFunc(idle, cancel)
	defer timer.Stop()

	req, err := s.newReq(ctx, "/api/file", url.Values{"key": {t.Key}})
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := s.http().Do(req)
	if err != nil {
		if parent.Err() == nil && ctx.Err() != nil {
			return errors.New("网络中断（30 秒无数据）")
		}
		return fmt.Errorf("连接失败：%v", err)
	}
	defer resp.Body.Close()
	var f *os.File
	switch {
	case resp.StatusCode == 206 && have > 0:
		f, err = os.OpenFile(part, os.O_WRONLY|os.O_APPEND, 0o644)
	case resp.StatusCode == 200:
		have = 0
		f, err = os.Create(part)
	case resp.StatusCode == 416 && have == t.Size && t.Size > 0:
		resp.Body.Close()
		return finishPart(part, target, t.Size)
	case resp.StatusCode == 401:
		return errors.New("配对码失效，请在手机上重新查看配对码后重新连接")
	case resp.StatusCode == 404:
		return errors.New("手机上找不到这首歌")
	default:
		return fmt.Errorf("手机返回 %d", resp.StatusCode)
	}
	if err != nil {
		return fmt.Errorf("无法写入文件：%v", err)
	}
	got := have
	progress(got)
	buf := make([]byte, 256*1024)
	r := &idleReader{r: resp.Body, timer: timer, idle: idle}
	lastReport := time.Now()
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return fmt.Errorf("写入失败：%v", werr)
			}
			got += int64(n)
			lim.wait(ctx, n)
			if time.Since(lastReport) > 150*time.Millisecond {
				progress(got)
				lastReport = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			progress(got)
			if parent.Err() == nil && ctx.Err() != nil {
				return errors.New("网络中断（30 秒无数据）")
			}
			return fmt.Errorf("下载中断：%v", rerr)
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	progress(got)
	return finishPart(part, target, t.Size)
}

func finishPart(part, target string, size int64) error {
	st, err := os.Stat(part)
	if err != nil {
		return err
	}
	if size > 0 && st.Size() != size {
		if st.Size() > size {
			os.Remove(part)
		}
		return fmt.Errorf("文件大小不对（%d / %d）", st.Size(), size)
	}
	if _, err := os.Stat(target); err == nil {
		return errors.New("目标文件已存在")
	}
	return os.Rename(part, target)
}

func fetchSmall(ctx context.Context, s *phoneSession, path, key string) ([]byte, error) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := s.newReq(c, path, url.Values{"key": {key}})
	if err != nil {
		return nil, err
	}
	resp, err := s.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

func syncExtras(ctx context.Context, s *phoneSession, t PhoneTrack, audioPath string) {
	// lyrics: next to the song, only if there is none yet
	if t.HasLrc && audioPath != "" {
		lrc := strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ".lrc"
		if sidecarLrc(audioPath) == "" {
			if b, err := fetchSmall(ctx, s, "/api/lrc", t.Key); err == nil && len(strings.TrimSpace(string(b))) > 0 {
				os.WriteFile(lrc+".tmp", b, 0o644)
				os.Rename(lrc+".tmp", lrc)
			}
		}
	}
	m := Meta{Title: strings.TrimSpace(t.Title), Artist: strings.TrimSpace(t.Artist), Album: strings.TrimSpace(t.Album)}
	if t.HasCover {
		if b, err := fetchSmall(ctx, s, "/api/cover", t.Key); err == nil && len(b) > 100 {
			name := "phone_" + hashStr(t.Key) + ".jpg"
			p := filepath.Join(dataDir, "covers", name)
			if os.WriteFile(p+".tmp", b, 0o644) == nil && os.Rename(p+".tmp", p) == nil {
				m.Cover = name
				m.CoverV = time.Now().UnixNano() / 1e6
			}
		}
	}
	metaMu.Lock()
	if old, ok := meta.Phone[t.Key]; ok && m.Cover == "" {
		m.Cover, m.CoverV = old.Cover, old.CoverV
	}
	meta.Phone[t.Key] = m
	metaMu.Unlock()
}

// importPlaylists merges the phone's playlists (by key) into the PC's.
func importPlaylists(pls []PhonePlaylist) int {
	if len(pls) == 0 {
		return 0
	}
	plMu.Lock()
	n := 0
	for _, pp := range pls {
		idx := -1
		for i, p := range playlists {
			if (pp.Fav && p.Fav) || (!pp.Fav && !p.Fav && p.Name == pp.Name) {
				idx = i
				break
			}
		}
		if idx < 0 {
			playlists = append(playlists, Playlist{ID: "pl" + strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.Itoa(n),
				Name: pp.Name, Keys: []string{}, Created: time.Now().Unix()})
			idx = len(playlists) - 1
		}
		have := map[string]bool{}
		for _, k := range playlists[idx].Keys {
			have[k] = true
		}
		for _, k := range pp.Keys {
			if !have[k] {
				playlists[idx].Keys = append(playlists[idx].Keys, k)
				have[k] = true
			}
		}
		n++
	}
	plMu.Unlock()
	savePlaylists()
	return n
}
