package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 内置更新
// Hosting: <base>/pc-version.json = {"version":"0.2.0","setup":"shiyin-pc-setup-0.2.0.exe","size":N,"sha256":"…","notes":"…"}
// and <base>/<setup>. The installer is run silently (/S); it closes this app, upgrades in place and relaunches it.

const defaultUpdateBase = "https://shiyin.example.com"

func updateBase() string {
	if u := os.Getenv("SHIYIN_UPDATE_URL"); u != "" { // tests only
		return strings.TrimRight(u, "/")
	}
	return defaultUpdateBase
}

type UpdateInfo struct {
	Version string `json:"version"`
	Setup   string `json:"setup"`
	Size    int64  `json:"size"`
	Sha256  string `json:"sha256"`
	Notes   string `json:"notes"`
}

// compareVersions returns -1, 0, 1 for "0.2.0"-style versions.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

var (
	setupNameRe = regexp.MustCompile(`^[A-Za-z0-9._\-]+\.exe$`)
	shaRe       = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

var updClient = &http.Client{Transport: updateTransport, Timeout: 50 * time.Second}

func fetchUpdateInfo(ctx context.Context) (*UpdateInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", updateBase()+"/pc-version.json?t="+strconv.FormatInt(time.Now().Unix(), 10), nil)
	req.Header.Set("User-Agent", "Shiyin-PC/"+appVersion)
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := updClient.Do(req)
	if err != nil {
		return nil, errors.New(friendlyNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("服务器上没有找到版本信息")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("服务器返回 HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, errors.New(friendlyNetErr(err))
	}
	var info UpdateInfo
	if err := json.Unmarshal(b, &info); err != nil || info.Version == "" {
		return nil, errors.New("版本信息格式不对")
	}
	if !setupNameRe.MatchString(info.Setup) || !shaRe.MatchString(info.Sha256) || info.Size <= 0 || info.Size > 200<<20 {
		return nil, errors.New("版本信息不完整")
	}
	info.Sha256 = strings.ToLower(info.Sha256)
	return &info, nil
}

type checkResult struct {
	OK        bool        `json:"ok"`
	HasUpdate bool        `json:"hasUpdate"`
	Current   string      `json:"current"`
	Info      *UpdateInfo `json:"info,omitempty"`
	Message   string      `json:"message,omitempty"`
	Cached    bool        `json:"cached,omitempty"`
	Skipped   bool        `json:"skipped,omitempty"`
}

type updCache struct {
	At   int64       `json:"at"`
	Info *UpdateInfo `json:"info"`
}

// checkUpdate: silent = launch check, at most once a day (the cached answer is reused in between).
func checkUpdate(silent bool) checkResult {
	res := checkResult{Current: appVersion}
	if silent {
		var c updCache
		if prefGet("updateCheck", &c) && time.Since(time.UnixMilli(c.At)) < 24*time.Hour && time.UnixMilli(c.At).Before(time.Now().Add(time.Minute)) {
			res.OK, res.Cached = true, true
			if c.Info != nil && compareVersions(c.Info.Version, appVersion) > 0 {
				res.HasUpdate, res.Info = true, c.Info
			}
			return res
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	info, err := fetchUpdateInfo(ctx)
	if err != nil {
		res.Message = err.Error()
		if silent { // try again on the next launch, but not more than every 2 hours
			prefSet("updateCheck", updCache{At: time.Now().Add(-22 * time.Hour).UnixMilli()})
		}
		return res
	}
	prefSet("updateCheck", updCache{At: time.Now().UnixMilli(), Info: info})
	res.OK = true
	if compareVersions(info.Version, appVersion) > 0 {
		res.HasUpdate, res.Info = true, info
	}
	return res
}

// ---------- download ----------

type UpdateStatus struct {
	Phase   string `json:"phase"` // idle | download | verify | ready | error | cancelled | installing
	Got     int64  `json:"got"`
	Total   int64  `json:"total"`
	Message string `json:"message,omitempty"`
	Version string `json:"version,omitempty"`
	Try     int    `json:"try,omitempty"`
}

var (
	updMu     sync.Mutex
	updStatus = UpdateStatus{Phase: "idle"}
	updCancel context.CancelFunc
	updFile   string
)

func getUpdateStatus() UpdateStatus {
	updMu.Lock()
	defer updMu.Unlock()
	return updStatus
}

func setUpd(f func(s *UpdateStatus)) {
	updMu.Lock()
	f(&updStatus)
	updMu.Unlock()
}

func updateDir() string { return filepath.Join(os.TempDir(), "ShiyinUpdate") }

// cleanupUpdateFiles removes installers left over from an earlier update (called at launch).
func cleanupUpdateFiles() {
	ents, _ := os.ReadDir(updateDir())
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 10*time.Minute {
			os.Remove(filepath.Join(updateDir(), e.Name()))
		}
	}
}

func startUpdateDownload() error {
	updMu.Lock()
	if updStatus.Phase == "download" || updStatus.Phase == "verify" || updStatus.Phase == "installing" {
		updMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	updCancel = cancel
	updStatus = UpdateStatus{Phase: "download"}
	updMu.Unlock()
	go func() {
		defer cancel()
		path, info, err := downloadUpdate(ctx)
		if err != nil {
			if ctx.Err() != nil {
				setUpd(func(s *UpdateStatus) { s.Phase, s.Message = "cancelled", "已取消下载" })
			} else {
				log.Printf("update: %v", err)
				setUpd(func(s *UpdateStatus) { s.Phase, s.Message = "error", err.Error() })
			}
			return
		}
		updMu.Lock()
		updFile = path
		updStatus.Phase, updStatus.Version, updStatus.Message = "ready", info.Version, "校验通过"
		updMu.Unlock()
	}()
	return nil
}

func cancelUpdate() {
	updMu.Lock()
	if updCancel != nil && (updStatus.Phase == "download" || updStatus.Phase == "verify") {
		updCancel()
	}
	updMu.Unlock()
}

func downloadUpdate(ctx context.Context) (string, *UpdateInfo, error) {
	info, err := fetchUpdateInfo(ctx)
	if err != nil {
		return "", nil, errors.New("检查更新失败：" + err.Error())
	}
	if compareVersions(info.Version, appVersion) <= 0 {
		return "", nil, errors.New("已是最新版本")
	}
	setUpd(func(s *UpdateStatus) { s.Total, s.Version = info.Size, info.Version })
	if err := os.MkdirAll(updateDir(), 0o755); err != nil {
		return "", nil, errors.New("无法写入临时文件夹：" + err.Error())
	}
	final := filepath.Join(updateDir(), info.Setup)
	part := final + ".part"
	if fileSHA(final) == info.Sha256 { // already downloaded earlier
		setUpd(func(s *UpdateStatus) { s.Got = info.Size })
		return final, info, nil
	}
	src := updateBase() + "/" + info.Setup
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		setUpd(func(s *UpdateStatus) { s.Try = attempt - 1 })
		lastErr = fetchPart(ctx, src, part, info.Size)
		if lastErr == nil {
			break
		}
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		var he httpErr
		if errors.As(lastErr, &he) && (he.code == 404 || he.code == 403) {
			break
		}
		select {
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	if lastErr != nil {
		var he httpErr
		if errors.As(lastErr, &he) && he.code == 404 {
			return "", nil, errors.New("下载失败：服务器上没有找到安装包")
		}
		return "", nil, errors.New("下载失败：" + lastErr.Error())
	}
	setUpd(func(s *UpdateStatus) { s.Phase = "verify" })
	if st, err := os.Stat(part); err != nil || st.Size() != info.Size {
		os.Remove(part)
		return "", nil, errors.New("下载不完整，请重试")
	}
	if fileSHA(part) != info.Sha256 {
		os.Remove(part)
		return "", nil, errors.New("安装包校验失败（SHA-256 不一致），已删除，请重试")
	}
	os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return "", nil, errors.New("无法保存安装包：" + err.Error())
	}
	return final, info, nil
}

type httpErr struct{ code int }

func (e httpErr) Error() string { return fmt.Sprintf("服务器返回 HTTP %d", e.code) }

// fetchPart downloads into part, resuming with Range when possible; 30 s without data = failure.
func fetchPart(ctx context.Context, src, part string, size int64) error {
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
		if have >= size {
			if have == size {
				return nil
			}
			os.Remove(part)
			have = 0
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, "GET", src, nil)
	req.Header.Set("User-Agent", "Shiyin-PC/"+appVersion)
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := (&http.Client{Transport: updateTransport}).Do(req) // no overall timeout: big file on a slow line
	if err != nil {
		return errors.New(friendlyNetErr(err))
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case 206:
		flags |= os.O_APPEND
	case 200:
		flags |= os.O_TRUNC
		have = 0
	default:
		return httpErr{resp.StatusCode}
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return errors.New("无法写入临时文件：" + err.Error())
	}
	defer f.Close()
	setUpd(func(s *UpdateStatus) { s.Got = have })
	idle := time.AfterFunc(30*time.Second, cancel)
	defer idle.Stop()
	buf := make([]byte, 64<<10)
	got := have
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(30 * time.Second)
			if got+int64(n) > size {
				return errors.New("安装包大小不对")
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return errors.New("写入失败：" + err.Error())
			}
			got += int64(n)
			g := got
			setUpd(func(s *UpdateStatus) { s.Got = g })
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() == nil && cctx.Err() != nil {
				return errors.New("连接超时（30 秒没有收到数据）")
			}
			return errors.New(friendlyNetErr(rerr))
		}
	}
	if got != size {
		return errors.New("连接中断，已下载 " + strconv.FormatInt(got*100/size, 10) + "%")
	}
	return nil
}

func fileSHA(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// installUpdate starts the downloaded installer silently and quits this app (the installer relaunches it).
func installUpdate(quit func()) error {
	updMu.Lock()
	p, phase := updFile, updStatus.Phase
	updMu.Unlock()
	if phase != "ready" || p == "" {
		return errors.New("安装包还没有下载好")
	}
	if fileSHA(p) == "" {
		return errors.New("找不到下载的安装包，请重新下载")
	}
	if err := runInstaller(p); err != nil {
		return errors.New("无法启动安装程序：" + err.Error())
	}
	setUpd(func(s *UpdateStatus) { s.Phase, s.Message = "installing", "正在安装…" })
	log.Printf("update: installer %s started, quitting", p)
	go func() {
		time.Sleep(600 * time.Millisecond)
		quit()
	}()
	return nil
}
