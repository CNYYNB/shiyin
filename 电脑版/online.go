package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---------- internet access ----------
// Windows apps written in Go ignore the system proxy (Clash / v2rayN "系统代理"), unlike browsers.
// webTransport uses: env HTTP(S)_PROXY → Windows system proxy → direct; and if the proxy itself
// can't be reached (VPN app closed), retries GET requests directly.

func isLocalHost(h string) bool {
	host := h
	if hh, _, err := net.SplitHostPort(h); err == nil {
		host = hh
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func proxyFor(req *http.Request) (*url.URL, error) {
	if isLocalHost(req.URL.Host) {
		return nil, nil
	}
	if u, err := http.ProxyFromEnvironment(req); u != nil || err != nil {
		return u, err
	}
	if p := systemProxy(req.URL.Scheme); p != "" {
		if !strings.Contains(p, "://") {
			p = "http://" + p
		}
		if u, err := url.Parse(p); err == nil {
			return u, nil
		}
	}
	return nil, nil
}

func newTransport(proxy func(*http.Request) (*url.URL, error)) *http.Transport {
	return &http.Transport{
		Proxy:                 proxy,
		DialContext:           (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}
}

type fallbackRT struct{ viaProxy, direct *http.Transport }

func (f *fallbackRT) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := f.viaProxy.RoundTrip(req)
	if err == nil || req.Method != "GET" || req.Context().Err() != nil {
		return resp, err
	}
	if u, _ := proxyFor(req); u == nil {
		return resp, err // no proxy was involved
	}
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "proxyconnect" || strings.Contains(err.Error(), "proxyconnect") {
		return f.direct.RoundTrip(req)
	}
	return resp, err
}

var webTransport http.RoundTripper = &fallbackRT{viaProxy: newTransport(proxyFor), direct: newTransport(nil)}

// friendlyNetErr turns Go network errors into short Chinese messages.
func friendlyNetErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "已取消"
	case errors.As(err, &ne) && ne.Timeout(), strings.Contains(s, "timeout"):
		return "连接超时，服务器暂时无法访问（可以开关一下 VPN 再试）"
	case strings.Contains(s, "no such host"):
		return "无法解析服务器地址，请检查网络"
	case strings.Contains(s, "proxyconnect"):
		return "无法连接系统代理，请检查 VPN / 代理软件"
	case strings.Contains(s, "connection refused"), strings.Contains(s, "actively refused"):
		return "连接被拒绝，请检查网络或代理"
	case strings.Contains(s, "connection reset"), strings.Contains(s, "forcibly closed"), strings.Contains(s, "EOF"):
		return "连接被中断，代理和直连都试过了，请换条线路或稍后再试"
	case strings.Contains(s, "certificate"), strings.Contains(s, "x509"), strings.Contains(s, "tls"):
		return "安全连接失败（网络可能被拦截），请换个网络或开关 VPN 再试"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return "网络错误：" + s
}

// ---------- Apple (iTunes) catalogue: Chinese names + cover art (port of the phone's Online.kt) ----------

func itunesBase() string {
	if u := os.Getenv("SHIYIN_ITUNES_URL"); u != "" { // tests only
		return strings.TrimRight(u, "/")
	}
	return "https://itunes.apple.com"
}

var (
	itMu    sync.Mutex
	itLast  time.Time
	itGap   = 3200 * time.Millisecond // ~20 requests/minute per IP
	itunesC = &http.Client{Transport: webTransport, Timeout: 15 * time.Second}
)

func init() {
	if os.Getenv("SHIYIN_ITUNES_URL") != "" {
		itGap = 30 * time.Millisecond
	}
}

var errTooFrequent = errors.New("查询太频繁，请稍后再试")

type itunesHit struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int64  `json:"duration"`
	Art      string `json:"art"`
}

func itunesSearch(ctx context.Context, term, country string) ([]itunesHit, error) {
	u := itunesBase() + "/search?term=" + url.QueryEscape(term) + "&entity=song&limit=10&country=" + country + "&lang=en_us"
	for attempt := 0; attempt < 2; attempt++ {
		itMu.Lock()
		wait := time.Until(itLast.Add(itGap))
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				itMu.Unlock()
				return nil, ctx.Err()
			}
		}
		itLast = time.Now()
		itMu.Unlock()
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		req.Header.Set("User-Agent", webUA)
		resp, err := itunesC.Do(req)
		if err != nil {
			return nil, errors.New(friendlyNetErr(err))
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode == 403 || resp.StatusCode == 429 {
			if attempt == 0 {
				itMu.Lock()
				itLast = time.Now().Add(45 * time.Second)
				itMu.Unlock()
				continue
			}
			return nil, errTooFrequent
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		var r struct {
			Results []struct {
				TrackName, ArtistName, CollectionName, ArtworkUrl100 string
				TrackTimeMillis                                      int64
			}
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, errors.New("返回数据无法解析")
		}
		out := make([]itunesHit, 0, len(r.Results))
		for _, x := range r.Results {
			out = append(out, itunesHit{x.TrackName, x.ArtistName, x.CollectionName, x.TrackTimeMillis, strings.Replace(x.ArtworkUrl100, "100x100bb", "600x600bb", 1)})
		}
		return out, nil
	}
	return nil, errTooFrequent
}

func hasCjk(s string) bool {
	for _, r := range s {
		if (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf) {
			return true
		}
	}
	return false
}

var itLiveRe = regexp.MustCompile(`(?i)\blive\b|現場|现场|演唱會|演唱会|piano|cover|伴奏|instrumental|karaoke|remix|\bdj\b`)

// bestIndex: the confident Chinese match (duration within 2.5 s, same live/cover-ness), or -1.
func bestIndex(arr []itunesHit, title string, durMs int64) int {
	best, bestDiff := -1, int64(1<<62)
	wantLive := itLiveRe.MatchString(title)
	for i, o := range arr {
		if !hasCjk(o.Title) && !hasCjk(o.Artist) {
			continue
		}
		if itLiveRe.MatchString(o.Title+" "+o.Album) != wantLive {
			continue
		}
		if durMs <= 0 || o.Duration <= 0 {
			continue
		}
		d := o.Duration - durMs
		if d < 0 {
			d = -d
		}
		if d <= 2500 && d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

// Apple's Hong Kong / Taiwan stores index the English names of Chinese songs but return the Chinese ones.
func itunesCandidates(ctx context.Context, title, artist string, durMs int64) ([]itunesHit, int, error) {
	if artist == "未知歌手" {
		artist = ""
	}
	join := func(a, b string) string { return strings.TrimSpace(strings.TrimSpace(a) + " " + strings.TrimSpace(b)) }
	terms := []string{join(title, artist)}
	if s := strings.TrimSpace(bracketRe.ReplaceAllString(title, "")); s != "" && s != title {
		terms = append(terms, join(s, artist))
	}
	var out []itunesHit
	seen := map[string]bool{}
	var lastErr error
	for i, term := range terms {
		if i > 0 && term == terms[0] {
			continue
		}
		for _, country := range []string{"hk", "tw"} {
			arr, err := itunesSearch(ctx, term, country)
			if err != nil {
				if err == errTooFrequent || ctx.Err() != nil {
					return out, bestIndex(out, title, durMs), err
				}
				lastErr = err
				continue
			}
			for _, o := range arr {
				k := o.Title + "|" + o.Artist + "|" + o.Album
				if !seen[k] {
					seen[k] = true
					out = append(out, o)
				}
			}
			if len(out) > 0 {
				if b := bestIndex(out, title, durMs); b >= 0 {
					return out, b, nil
				}
			}
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, -1, lastErr
	}
	return out, bestIndex(out, title, durMs), nil
}

// downloadImage fetches a cover image (max 8 MB).
func downloadImage(ctx context.Context, u string) ([]byte, error) {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return nil, errors.New("图片地址无效")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", webUA)
	resp, err := (&http.Client{Transport: webTransport, Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, errors.New(friendlyNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("下载封面失败：HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, errors.New(friendlyNetErr(err))
	}
	if !strings.HasPrefix(http.DetectContentType(b), "image/") {
		return nil, errors.New("下载的不是图片")
	}
	return b, nil
}

// ---------- update downloads: try every route before giving up ----------
// Some VPN lines reset connections to pages.dev, others choke on HTTP/2, and a line that is
// down still leaves the system proxy set. Update requests (GET only) therefore try, in order:
// system proxy (HTTP/2) → system proxy (HTTP/1.1) → direct (HTTP/1.1) → system proxy again,
// moving on after any network error. Without a system proxy: direct, direct HTTP/1.1, direct.

func newTransportH1(proxy func(*http.Request) (*url.URL, error)) *http.Transport {
	t := newTransport(proxy)
	t.ForceAttemptHTTP2 = false
	t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	return t
}

type multiRouteRT struct {
	proxyH2, proxyH1, directH2, directH1 *http.Transport
	pause                                time.Duration
	pick                                 func(*http.Request) []*http.Transport // tests
}

var updateTransport http.RoundTripper = &multiRouteRT{
	proxyH2: newTransport(proxyFor), proxyH1: newTransportH1(proxyFor),
	directH2: newTransport(nil), directH1: newTransportH1(nil), pause: 600 * time.Millisecond,
}

func (m *multiRouteRT) routes(req *http.Request) []*http.Transport {
	if m.pick != nil {
		return m.pick(req)
	}
	if u, _ := proxyFor(req); u != nil {
		return []*http.Transport{m.proxyH2, m.proxyH1, m.directH1, m.proxyH2}
	}
	return []*http.Transport{m.directH2, m.directH1, m.directH2}
}

func (m *multiRouteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != "GET" {
		return m.proxyH2.RoundTrip(req)
	}
	var lastErr error
	for i, t := range m.routes(req) {
		if i > 0 {
			select {
			case <-req.Context().Done():
				return nil, lastErr
			case <-time.After(m.pause):
			}
		}
		resp, err := t.RoundTrip(req.Clone(req.Context()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if req.Context().Err() != nil {
			return nil, err
		}
	}
	return nil, lastErr
}
