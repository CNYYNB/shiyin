package main

// 0.2.3 电脑热点安全措施 (a user's laptop hard-froze while sending songs over the Windows Mobile Hotspot:
// Intel AX201 driver Netwtw14 event 6062 in a loop, then Kernel-Power 41; a Clash TUN adapter was also up):
//   - before turning the hotspot on, warn about active TUN/VPN adapters (the user may continue)
//   - remember (prefs.json hotspotOwned) that 拾音 turned it on; turn it off 60 s after the import
//     finished, after 30 min unused, when 拾音 exits normally, and on the next start after a crash
//   - imports going over the hotspot run one at a time, limited to ~6 MB/s (less driver stress)

import (
	"context"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

var (
	hotspotIdleOff  = 60 * time.Second  // after the import finished
	hotspotUnused   = 30 * time.Minute  // turned on but never used
	hotspotSlowRate = 6.0 * 1024 * 1024 // bytes/s while the transfer goes over the PC hotspot
)

var hsMu sync.Mutex

type hotspotOwnedPref struct {
	On    bool  `json:"on"`
	Since int64 `json:"since"` // unix seconds
}

func hotspotOwned() (bool, time.Time) {
	var p hotspotOwnedPref
	if !prefGet("hotspotOwned", &p) || !p.On {
		return false, time.Time{}
	}
	return true, time.Unix(p.Since, 0)
}

func setHotspotOwned(on bool) {
	if on {
		prefSet("hotspotOwned", hotspotOwnedPref{On: true, Since: time.Now().Unix()})
	} else if ok, _ := hotspotOwned(); ok {
		prefSet("hotspotOwned", nil)
	}
}

// hotspotTurnOn: TUN/VPN check first (unless force), then start; remembers that we started it.
func hotspotTurnOn(force bool) HotspotStatus {
	hsMu.Lock()
	defer hsMu.Unlock()
	if !force {
		if t := tunAdapters(); len(t) > 0 {
			st := hotspotDo("status")
			if !st.On {
				st.TunWarn = t
				st.NeedConfirm = true
				return st
			}
		}
	}
	before := hotspotDo("status")
	if before.On {
		before.Owned, _ = hotspotOwned()
		return before // already on (the user's own hotspot): never turn it off behind their back
	}
	st := hotspotDo("on")
	if st.On {
		setHotspotOwned(true)
		st.Owned = true
		log.Printf("hotspot: turned on by 拾音")
	}
	return st
}

func hotspotTurnOff(reason string) HotspotStatus {
	hsMu.Lock()
	defer hsMu.Unlock()
	st := hotspotDo("off")
	if !st.On {
		setHotspotOwned(false)
	}
	log.Printf("hotspot: off (%s) → on=%v err=%s", reason, st.On, st.Error)
	return st
}

func hotspotStatus() HotspotStatus {
	st := hotspotDo("status")
	if o, _ := hotspotOwned(); o && st.On {
		st.Owned = true
	}
	st.AutoOff = int(hotspotIdleOff / time.Second)
	return st
}

// hotspotShouldOff decides whether a hotspot 拾音 turned on should go off now (pure; unit-tested).
func hotspotShouldOff(owned bool, since time.Time, running bool, lastJobEnd time.Time, now time.Time) (bool, string) {
	if !owned || running {
		return false, ""
	}
	if !lastJobEnd.IsZero() && lastJobEnd.After(since) && now.Sub(lastJobEnd) >= hotspotIdleOff {
		return true, "传输完成"
	}
	last := since
	if lastJobEnd.After(last) {
		last = lastJobEnd
	}
	if now.Sub(last) >= hotspotUnused {
		return true, "长时间未使用"
	}
	return false, ""
}

// hotspotWatch runs for the life of the app.
func hotspotWatch() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for range t.C {
		owned, since := hotspotOwned()
		if !owned {
			continue
		}
		j := importStatus()
		var end time.Time
		if j.Finished > 0 {
			end = time.Unix(j.Finished, 0)
		}
		if off, why := hotspotShouldOff(owned, since, j.Running, end, time.Now()); off {
			hotspotTurnOff(why)
		}
	}
}

// hotspotRecover: the app crashed / was killed while its hotspot was on → turn it off now.
func hotspotRecover() {
	if owned, _ := hotspotOwned(); !owned {
		return
	}
	if st := hotspotDo("status"); st.On {
		hotspotTurnOff("上次退出时未关闭")
	} else {
		setHotspotOwned(false)
	}
}

// hotspotOnExit: normal exit with our hotspot on → turn it off.
func hotspotOnExit() {
	if owned, _ := hotspotOwned(); owned {
		hotspotTurnOff("退出拾音")
	}
}

// ---------------- TUN / VPN adapters ----------------

var tunWords = []string{"tun", "tunnel", "wintun", "tap-", "tap ", "clash", "mihomo", "meta", "wireguard", "sing-box", "singbox",
	"v2ray", "xray", "nekoray", "nekobox", "hysteria", "vpn", "wg"}

// tunLike: adapter name/description of a proxy TUN or VPN (pure; unit-tested). The Windows hotspot
// adapter ("本地连接*", "Wi-Fi Direct Virtual Adapter") and ordinary Wi-Fi/Ethernet never match.
func tunLike(name, desc string) bool {
	n := strings.ToLower(name + " " + desc + " ")
	if strings.Contains(n, "wi-fi direct") || strings.Contains(n, "本地连接*") || strings.Contains(n, "local area connection*") {
		return false
	}
	for _, w := range tunWords {
		if w == "wg" {
			if strings.HasPrefix(strings.ToLower(name), "wg") {
				return true
			}
			continue
		}
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// tunAdapters lists active adapters that look like a proxy TUN / VPN ("name（description）").
func tunAdapters() []string {
	infos := adapterInfos()
	ifs, _ := net.Interfaces()
	var out []string
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		hasV4, fake := false, false
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				hasV4 = true
				ip4 := ipn.IP.To4()
				fake = fake || (ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19))
			}
		}
		if !hasV4 {
			continue
		}
		ai := infos[ifc.Name]
		if fake || tunLike(ifc.Name, ai.Desc) {
			s := ifc.Name
			if ai.Desc != "" && !strings.EqualFold(ai.Desc, ifc.Name) {
				s += "（" + ai.Desc + "）"
			}
			out = append(out, s)
		}
	}
	return out
}

// ---------------- transfer over the PC hotspot ----------------

func inHotspotNet(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 192 && ip4[1] == 168 && ip4[2] == 137
}

// viaPCHotspot: does traffic to this phone address go over the PC's own Windows hotspot?
func viaPCHotspot(peer net.IP) bool {
	if peer == nil {
		return false
	}
	if inHotspotNet(peer) {
		return true
	}
	lip := localIPFor(peer)
	if lip == nil {
		return false
	}
	if inHotspotNet(lip) {
		return true
	}
	infos := adapterInfos()
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(lip) {
				return lanKind(ifc.Name, lip, infos[ifc.Name]) == "hotspot"
			}
		}
	}
	return false
}

func (s *phoneSession) peerIP() net.IP {
	if s.reverse {
		rev.mu.Lock()
		defer rev.mu.Unlock()
		if rev.phone != nil {
			return net.ParseIP(rev.phone.IP)
		}
		return nil
	}
	if h, _, err := net.SplitHostPort(strings.TrimPrefix(s.base, "http://")); err == nil {
		return net.ParseIP(h)
	}
	return nil
}

// rateLimiter keeps an average of at most rate bytes/s since its start.
type rateLimiter struct {
	rate  float64
	start time.Time
	n     int64
}

func newRateLimiter(rate float64) *rateLimiter { return &rateLimiter{rate: rate, start: time.Now()} }

func (r *rateLimiter) wait(ctx context.Context, n int) {
	if r == nil || r.rate <= 0 {
		return
	}
	r.n += int64(n)
	due := r.start.Add(time.Duration(float64(r.n) / r.rate * float64(time.Second)))
	if d := time.Until(due); d > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}
}
