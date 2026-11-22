package main

// 「手机连电脑」(0.2.1): reverse connection for networks where the PC cannot reach the phone
// (router AP isolation, phone VPN, HyperOS blocking inbound connections…).
//
// While the 从手机导入 dialog is open the PC listens on TCP 18522 (all interfaces) and answers
// UDP discovery on 18523. The phone opens a few outbound TCP connections, each starting with a
// one-line handshake carrying the 6-digit code shown on the PC. Accepted connections wait in a
// pool (the PC sends a blank keep-alive line every 10 s); to make an API request the PC takes one
// connection from the pool and speaks plain HTTP/1.1 on it, exactly as if it had dialled the phone.
// The phone answers with Connection: close and opens a fresh connection to refill the pool.
// So the whole existing import (phone.go) runs unchanged on top of an http.Client whose dialer
// pulls from the pool. Protocol: shiyin-sync-protocol.md「反向连接」.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	reversePort    = 18522
	reverseUDPPort = 18523
	reverseBeatTTL = 20 * time.Second // listener closes this long after the dialog stopped polling
)

type revPhone struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Version string `json:"version"`
	Count   int    `json:"count"`
	Since   int64  `json:"since"`
}

type reverseHub struct {
	mu       sync.Mutex
	ln       net.Listener
	udp      *net.UDPConn
	port     int
	udpOK    bool
	code     string
	beat     time.Time
	lastUse  time.Time
	idle     []net.Conn
	notify   chan struct{}
	phone    *revPhone
	lastSeen time.Time
	stopCh   chan struct{}
}

var rev = &reverseHub{notify: make(chan struct{}, 1)}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func newCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(900000))
	return strconv.FormatInt(n.Int64()+100000, 10)
}

// ---------------- LAN addresses ----------------

type LanIP struct {
	IP    string `json:"ip"`
	Iface string `json:"iface"`
	Kind  string `json:"kind"`           // wifi | eth | hotspot | other
	Label string `json:"label"`          // Wi-Fi / 有线 / 电脑热点 / ""
	GW    bool   `json:"gw"`             // adapter has a default gateway (= the PC's real network)
	GwIP  string `json:"gwIp,omitempty"` // 0.2.3: the router's address (for the 关闭 AP 隔离 guide)
}

// adapterInfo is what the OS tells us about an adapter beyond net.Interface (see netinfo_*.go).
type adapterInfo struct {
	Gateway   bool
	GatewayIP string
	IfType    uint32 // IANA ifType: 6 ethernet, 71 IEEE 802.11
	Desc      string
	Known     bool
}

var virtualIfaceWords = []string{"vpn", "tun", "tap", "wintun", "clash", "mihomo", "meta", "vethernet", "vmware", "virtualbox",
	"hyper-v", "wsl", "loopback", "zerotier", "tailscale", "docker", "utun", "wireguard", "wg", "pseudo", "teredo", "isatap",
	"bluetooth", "蓝牙", "npcap", "radmin", "hamachi", "sangfor", "easyconnect", "openvpn", "virtual", "虚拟"}

// skipLanIP reports whether ip should not be offered to the phone (TUN fake-IP ranges, link-local, CGNAT…).
func skipLanIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() || ip4.IsMulticast() {
		return true
	}
	if ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19) { // 198.18.0.0/15: Clash / sing-box TUN
		return true
	}
	if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 { // 100.64.0.0/10: CGNAT / Tailscale
		return true
	}
	return false
}

func virtualIface(name string) bool {
	n := strings.ToLower(name)
	for _, w := range virtualIfaceWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// lanKind classifies an adapter. Windows' Mobile Hotspot / ICS adapter ("本地连接* 10",
// "Microsoft Wi-Fi Direct Virtual Adapter", always 192.168.137.1) is "hotspot": a phone on the PC's
// own hotspot can use it, a phone on the home Wi-Fi can't.
func lanKind(name string, ip net.IP, ai adapterInfo) string {
	n := strings.ToLower(name + " " + ai.Desc)
	ip4 := ip.To4()
	if (ip4 != nil && ip4[0] == 192 && ip4[1] == 168 && ip4[2] == 137) || strings.Contains(n, "wi-fi direct") ||
		strings.Contains(n, "hosted network") || strings.Contains(n, "本地连接*") || strings.Contains(n, "local area connection*") {
		return "hotspot"
	}
	switch {
	case ai.IfType == 71 || strings.Contains(n, "wlan") || strings.Contains(n, "wi-fi") || strings.Contains(n, "wifi") ||
		strings.Contains(n, "wireless") || strings.Contains(n, "无线"):
		return "wifi"
	case ai.IfType == 6 && !strings.Contains(n, "本地连接*") || strings.Contains(n, "以太网") || strings.Contains(n, "ethernet") ||
		strings.HasPrefix(n, "eth") || strings.HasPrefix(n, "en"):
		return "eth"
	}
	return "other"
}

var lanLabels = map[string]string{"wifi": "Wi-Fi", "eth": "有线", "hotspot": "电脑热点", "other": ""}

// lanRank: lower is better. The adapter with the default gateway (the PC's real network) first,
// then Wi-Fi, wired, others; the Windows hotspot adapter always last.
func lanRank(kind string, ip net.IP, gw bool) int {
	r := map[string]int{"wifi": 0, "eth": 1, "other": 3, "hotspot": 50}[kind]
	if kind == "hotspot" && hotspotOn.Load() { // 0.2.2: our hotspot is on → the phone should use it
		return -1
	}
	if kind != "hotspot" && !gw {
		r += 20
	}
	if !ip.IsPrivate() {
		r += 10
	}
	return r
}

type lanCand struct {
	Name string
	IP   net.IP
	Info adapterInfo
}

// rankLanIPs sorts candidates best first (pure; unit-tested).
func rankLanIPs(cs []lanCand) []LanIP {
	type cand struct {
		LanIP
		rank int
	}
	var out []cand
	anyGW := false
	for _, c := range cs {
		anyGW = anyGW || c.Info.Gateway
	}
	for _, c := range cs {
		k := lanKind(c.Name, c.IP, c.Info)
		gw := c.Info.Gateway || !anyGW // no gateway info at all: don't penalise anyone
		out = append(out, cand{LanIP{c.IP.To4().String(), c.Name, k, lanLabels[k], c.Info.Gateway, c.Info.GatewayIP}, lanRank(k, c.IP, gw)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	res := []LanIP{}
	seen := map[string]bool{}
	for _, c := range out {
		if !seen[c.IP] {
			seen[c.IP] = true
			res = append(res, c.LanIP)
		}
	}
	return res
}

// lanIPs lists the PC's own LAN IPv4 addresses, best first, without TUN/virtual adapters.
func lanIPs() []LanIP {
	infos := adapterInfos()
	var cs []lanCand
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		ai := infos[ifc.Name]
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtualIface(ifc.Name) {
			continue
		}
		if ai.Desc != "" && virtualIface(ai.Desc) && !strings.Contains(strings.ToLower(ai.Desc), "wi-fi direct") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || skipLanIP(ipn.IP) {
				continue
			}
			cs = append(cs, lanCand{ifc.Name, ipn.IP.To4(), ai})
		}
	}
	return rankLanIPs(cs)
}

// localIPFor returns the PC's address on the same subnet as dst (nil if none), used both to bind
// outgoing LAN connections to the real adapter (so a TUN/VPN route can't capture them) and to
// tell a phone which of our IPs it reached.
func localIPFor(dst net.IP) net.IP {
	dst = dst.To4()
	if dst == nil {
		return nil
	}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || skipLanIP(ipn.IP) {
				continue
			}
			if ipn.Contains(dst) && !ipn.IP.Equal(dst) {
				return ipn.IP.To4()
			}
		}
	}
	return nil
}

// lanDial dials LAN hosts directly from the matching adapter (never through a proxy).
func lanDial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if h, _, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(h); ip != nil {
			if lip := localIPFor(ip); lip != nil {
				d.LocalAddr = &net.TCPAddr{IP: lip}
				if c, err := d.DialContext(ctx, network, addr); err == nil {
					return c, nil
				}
				d.LocalAddr = nil // binding failed (odd adapter setup): plain dial
			}
		}
	}
	return d.DialContext(ctx, network, addr)
}

// ---------------- hub ----------------

type ReverseStatus struct {
	Listening bool      `json:"listening"`
	Port      int       `json:"port"`
	UDP       bool      `json:"udp"`
	Code      string    `json:"code"`
	IPs       []LanIP   `json:"ips"`
	Phone     *revPhone `json:"phone"`
	Conns     int       `json:"conns"`
	Error     string    `json:"error,omitempty"`
}

func (h *reverseHub) statusLocked() ReverseStatus {
	st := ReverseStatus{Listening: h.ln != nil, Port: h.port, UDP: h.udpOK, Code: h.code, IPs: lanIPs(), Conns: len(h.idle)}
	if h.phone != nil && (len(h.idle) > 0 || time.Since(h.lastSeen) < 15*time.Second) {
		cp := *h.phone
		st.Phone = &cp
	}
	return st
}

// start opens the listener (if not open yet) and records a heartbeat from the dialog.
func (h *reverseHub) start() (ReverseStatus, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.beat = time.Now()
	if h.ln != nil {
		return h.statusLocked(), nil
	}
	base := envInt("SHIYIN_REVERSE_PORT", reversePort)
	var ln net.Listener
	var err error
	for p := base; p < base+8; p++ {
		ln, err = net.Listen("tcp4", fmt.Sprintf(":%d", p))
		if err == nil {
			h.port = p
			break
		}
	}
	if ln == nil {
		return h.statusLocked(), fmt.Errorf("无法监听端口 %d：%v", base, err)
	}
	h.ln = ln
	if h.code == "" {
		h.code = newCode()
	}
	h.stopCh = make(chan struct{})
	go h.acceptLoop(ln)
	h.udpOK = false
	if u, err := net.ListenUDP("udp4", &net.UDPAddr{Port: envInt("SHIYIN_REVERSE_UDP", reverseUDPPort)}); err == nil {
		h.udp = u
		h.udpOK = true
		go h.udpLoop(u, h.port)
	} else {
		log.Printf("reverse udp: %v", err)
	}
	go h.janitor(h.stopCh)
	log.Printf("reverse: listening on %d", h.port)
	return h.statusLocked(), nil
}

func (h *reverseHub) heartbeat() ReverseStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ln != nil {
		h.beat = time.Now()
	}
	return h.statusLocked()
}

func (h *reverseHub) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
}

func (h *reverseHub) stopLocked() {
	if h.ln != nil {
		h.ln.Close()
		h.ln = nil
	}
	if h.udp != nil {
		h.udp.Close()
		h.udp = nil
	}
	if h.stopCh != nil {
		close(h.stopCh)
		h.stopCh = nil
	}
	for _, c := range h.idle {
		c.Close()
	}
	h.idle = nil
	h.phone = nil
	h.code = "" // a new code next time the dialog opens
	log.Printf("reverse: stopped")
}

// janitor keeps idle pool connections alive and closes everything once the dialog is gone
// (no heartbeat for reverseBeatTTL) and no import / request is using the phone.
func (h *reverseHub) janitor(stop chan struct{}) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		busy := importStatus().Running
		h.mu.Lock()
		if h.ln == nil {
			h.mu.Unlock()
			return
		}
		if !busy && time.Since(h.beat) > reverseBeatTTL && time.Since(h.lastUse) > reverseBeatTTL {
			h.stopLocked()
			h.mu.Unlock()
			return
		}
		keep := h.idle[:0]
		for _, c := range h.idle {
			c.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if _, err := c.Write([]byte("\r\n")); err != nil {
				c.Close()
				continue
			}
			c.SetWriteDeadline(time.Time{})
			keep = append(keep, c)
		}
		h.idle = keep
		h.mu.Unlock()
	}
}

func (h *reverseHub) udpLoop(u *net.UDPConn, port int) {
	buf := make([]byte, 1024)
	for {
		n, from, err := u.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !strings.HasPrefix(strings.TrimSpace(string(buf[:n])), "SHIYIN_FIND_PC") {
			continue
		}
		ip := ""
		if lip := localIPFor(from.IP); lip != nil {
			ip = lip.String()
		} else if l := lanIPs(); len(l) > 0 {
			ip = l[0].IP
		}
		host, _ := os.Hostname()
		b, _ := json.Marshal(map[string]any{"app": "shiyin-pc", "name": host, "ip": ip, "port": port, "version": appVersion, "needCode": true})
		u.WriteToUDP(b, from)
	}
}

// readLineRaw reads one \n-terminated line byte by byte (nothing beyond it is consumed).
func readLineRaw(c net.Conn, max int) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for len(b) < max {
		n, err := c.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				return strings.TrimRight(string(b), "\r"), nil
			}
			b = append(b, one[0])
		}
		if err != nil {
			return string(b), err
		}
	}
	return string(b), errors.New("line too long")
}

func (h *reverseHub) acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go h.handshake(c)
	}
}

func (h *reverseHub) handshake(c net.Conn) {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := readLineRaw(c, 4096)
	if err != nil {
		c.Close()
		return
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 || parts[0] != "SHIYIN-REVERSE/1" {
		c.Close()
		return
	}
	var hello struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Count   int    `json:"count"`
	}
	if len(parts) == 3 {
		json.Unmarshal([]byte(parts[2]), &hello)
	}
	h.mu.Lock()
	if h.ln == nil || parts[1] != h.code {
		h.mu.Unlock()
		c.Write([]byte("ERR badcode 配对码不对\n"))
		c.Close()
		return
	}
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	if h.phone == nil || h.phone.IP != ip || h.phone.Name != hello.Name {
		for _, o := range h.idle { // a different phone took over: drop the old one's pool
			o.Close()
		}
		h.idle = nil
		h.phone = &revPhone{Name: hello.Name, IP: ip, Version: hello.Version, Count: hello.Count, Since: time.Now().Unix()}
		log.Printf("reverse: phone %s (%s) connected", hello.Name, ip)
	}
	h.phone.Count = hello.Count
	h.lastSeen = time.Now()
	host, _ := os.Hostname()
	b, _ := json.Marshal(map[string]any{"app": "shiyin-pc", "name": host, "version": appVersion})
	c.SetDeadline(time.Time{})
	if _, err := c.Write(append(append([]byte("OK "), b...), '\n')); err != nil {
		h.mu.Unlock()
		c.Close()
		return
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(15 * time.Second)
	}
	h.idle = append(h.idle, c)
	h.mu.Unlock()
	select {
	case h.notify <- struct{}{}:
	default:
	}
}

// take hands out one pooled phone connection (waits up to 20 s for the phone to open one).
func (h *reverseHub) take(ctx context.Context) (net.Conn, error) {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		h.mu.Lock()
		if h.ln == nil && len(h.idle) == 0 {
			h.mu.Unlock()
			return nil, errors.New("手机连电脑已关闭")
		}
		if n := len(h.idle); n > 0 {
			c := h.idle[n-1]
			h.idle = h.idle[:n-1]
			h.lastUse = time.Now()
			h.lastSeen = time.Now()
			h.mu.Unlock()
			return c, nil
		}
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, errors.New("手机没有连过来（20 秒），请确认手机拾音的「传到电脑」页面还开着")
		case <-h.notify:
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// retryRT retries a request that failed before any response arrived (a pooled connection the
// phone had already dropped). Only body-less requests (all phone API calls are GETs).
type retryRT struct{ rt http.RoundTripper }

func (r retryRT) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := r.rt.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if req.Body != nil && req.Body != http.NoBody || req.Context().Err() != nil {
			break
		}
	}
	return nil, lastErr
}

var reverseClient = &http.Client{Transport: retryRT{&http.Transport{
	Proxy:                 nil,
	DialContext:           func(ctx context.Context, _, _ string) (net.Conn, error) { return rev.take(ctx) },
	DisableKeepAlives:     true, // the phone closes after every response
	ResponseHeaderTimeout: 60 * time.Second,
	MaxConnsPerHost:       0,
}}}

// connectReverse reads the library of the phone currently connected through the pool.
func connectReverse() (*PhoneLibrary, error) {
	rev.mu.Lock()
	ph0 := rev.phone
	listening := rev.ln != nil
	rev.mu.Unlock()
	if !listening {
		return nil, errors.New("「手机连电脑」没有开启，请重新打开「从手机导入」")
	}
	if ph0 == nil {
		return nil, errors.New("手机还没有连过来")
	}
	s := &phoneSession{base: "http://shiyin-phone", code: "000000", client: reverseClient, reverse: true}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, _ := s.newReq(ctx, "/api/library", nil)
	resp, err := s.http().Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("读取手机曲库超时（90 秒），请重试")
		}
		return nil, fmt.Errorf("读取手机曲库失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("手机返回错误 %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var lib PhoneLibrary
	if err := json.NewDecoder(resp.Body).Decode(&lib); err != nil {
		return nil, fmt.Errorf("手机曲库格式不对：%v", err)
	}
	if lib.Name == "" {
		lib.Name = ph0.Name
	}
	s.lib = lib
	phMu.Lock()
	ph = s
	phMu.Unlock()
	markPresent(&s.lib)
	return &s.lib, nil
}

func (h *reverseHub) status() ReverseStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.statusLocked()
}
