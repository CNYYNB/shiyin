package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestTunLike(t *testing.T) {
	yes := [][2]string{{"SakuraiTunnel", "Meta Tunnel"}, {"Clash", "Clash Tunnel"}, {"以太网 3", "Wintun Userspace Tunnel"},
		{"以太网 2", "TAP-Windows Adapter V9"}, {"wg0", ""}, {"sing-box", "sing-box TUN"}, {"v2rayN", ""}, {"Mihomo", ""},
		{"以太网 5", "WireGuard Tunnel"}, {"OpenVPN Connect", ""}}
	for _, c := range yes {
		if !tunLike(c[0], c[1]) {
			t.Errorf("want TUN: %v", c)
		}
	}
	no := [][2]string{{"WLAN", "Intel(R) Wi-Fi 6 AX201 160MHz"}, {"以太网", "Realtek PCIe GbE Family Controller"},
		{"本地连接* 10", "Microsoft Wi-Fi Direct Virtual Adapter #2"}, {"蓝牙网络连接", "Bluetooth Device (Personal Area Network)"},
		{"vEthernet (WSL)", "Hyper-V Virtual Ethernet Adapter"}}
	for _, c := range no {
		if tunLike(c[0], c[1]) {
			t.Errorf("not TUN: %v", c)
		}
	}
}

func TestHotspotShouldOff(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	cases := []struct {
		owned, running bool
		end, now       time.Time
		want           bool
		why            string
	}{
		{false, false, t0.Add(time.Minute), t0.Add(time.Hour), false, ""},                           // not ours
		{true, true, time.Time{}, t0.Add(2 * time.Hour), false, ""},                                 // still importing
		{true, false, time.Time{}, t0.Add(5 * time.Minute), false, ""},                              // waiting for the phone
		{true, false, time.Time{}, t0.Add(31 * time.Minute), true, "长时间未使用"},                        // never used
		{true, false, t0.Add(10 * time.Minute), t0.Add(10*time.Minute + 30*time.Second), false, ""}, // just finished
		{true, false, t0.Add(10 * time.Minute), t0.Add(11*time.Minute + time.Second), true, "传输完成"},
		{true, false, t0.Add(-time.Minute), t0.Add(2 * time.Minute), false, ""}, // job from before the hotspot
	}
	for i, c := range cases {
		off, why := hotspotShouldOff(c.owned, t0, c.running, c.end, c.now)
		if off != c.want || why != c.why {
			t.Errorf("case %d: got %v %q", i, off, why)
		}
	}
}

func TestViaPCHotspot(t *testing.T) {
	if !viaPCHotspot(net.ParseIP("192.168.137.23")) || viaPCHotspot(net.ParseIP("192.168.43.1")) || viaPCHotspot(nil) {
		t.Fatal("hotspot subnet")
	}
	s := &phoneSession{base: "http://192.168.137.80:18520"}
	if !viaPCHotspot(s.peerIP()) {
		t.Fatal("session peer")
	}
}

func TestRateLimiter(t *testing.T) {
	r := newRateLimiter(4 << 20) // 4 MB/s
	start := time.Now()
	for i := 0; i < 8; i++ {
		r.wait(context.Background(), 256<<10) // 2 MB total → ~0.5 s
	}
	if d := time.Since(start); d < 400*time.Millisecond || d > 1500*time.Millisecond {
		t.Fatalf("2 MB at 4 MB/s took %v", d)
	}
	var nilLim *rateLimiter
	nilLim.wait(context.Background(), 1<<30) // no limit, no panic
}

func TestHotspotOwnedFlag(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = old }()
	setHotspotOwned(true)
	if ok, since := hotspotOwned(); !ok || time.Since(since) > time.Minute {
		t.Fatal("owned not stored")
	}
	setHotspotOwned(false)
	if ok, _ := hotspotOwned(); ok {
		t.Fatal("owned not cleared")
	}
	if strings.Contains(string(getPrefs()["hotspotOwned"]), "true") {
		t.Fatal("pref left behind")
	}
}
