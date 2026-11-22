package main

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestDiagVerdict(t *testing.T) {
	// the user's real case: ping 192.168.0.1 ok, ping 192.168.0.104 → 来自 192.168.0.103 的回复: 无法访问目标主机
	winOut := "正在 Ping 192.168.0.104 具有 32 字节的数据:\r\n来自 192.168.0.103 的回复: 无法访问目标主机。\r\n来自 192.168.0.103 的回复: 无法访问目标主机。\r\n"
	if parsePingOK(winOut) || !pingNoHost(winOut) {
		t.Fatal("ping parse")
	}
	v := diagVerdict(diagFacts{SameSubnet: true, GatewayPing: 1, PhonePing: 0, PingNoHost: true, ArpOK: 0, TCP: "timeout"})
	if !strings.Contains(v, "路由器开了 AP 隔离") || !strings.Contains(v, "ARP 失败") || !strings.Contains(v, "无法访问目标主机") ||
		!strings.Contains(v, "① 手机开热点") || !strings.Contains(v, "④ 电脑开热点（不推荐") {
		t.Errorf("isolation verdict: %s", v)
	}
	cases := []struct {
		f    diagFacts
		want string
	}{
		{diagFacts{SameSubnet: false}, "不在同一个网络"},
		{diagFacts{SameSubnet: true, RouteVirtual: true}, "VPN"},
		{diagFacts{SameSubnet: true, TCP: "ok"}, "网络是通的"},
		{diagFacts{SameSubnet: true, GatewayPing: 1, PhonePing: 1, TCP: "refused"}, "手机服务未运行"},
		{diagFacts{SameSubnet: true, GatewayPing: 1, PhonePing: 0, ArpOK: 1, TCP: "timeout"}, "AP 隔离"},
		{diagFacts{SameSubnet: true, GatewayPing: 1, PhonePing: 1, ArpOK: 1, TCP: "timeout"}, "拦截了入站"},
		{diagFacts{SameSubnet: true, GatewayPing: 0, PhonePing: 0, ArpOK: -1, TCP: "timeout"}, "电脑这边"},
		{diagFacts{SameSubnet: true, TCP: "ok", PublicFW: true}, "公用网络"},
	}
	for _, c := range cases {
		if v := diagVerdict(c.f); !strings.Contains(v, c.want) {
			t.Errorf("%+v → %s (want %s)", c.f, v, c.want)
		}
	}
}

func TestDiagParsers(t *testing.T) {
	arp := "\r\n接口: 192.168.0.103 --- 0x7\r\n  Internet 地址         物理地址              类型\r\n  192.168.0.1           a4-39-b3-11-22-33     动态\r\n"
	if parseArp(arp, "192.168.0.1") != "a4-39-b3-11-22-33" || parseArp(arp, "192.168.0.104") != "" {
		t.Error("arp windows")
	}
	if parseArp("未找到 ARP 项。", "192.168.0.104") != "" {
		t.Error("arp none")
	}
	lin := "IP address       HW type     Flags       HW address            Mask     Device\n192.168.0.104    0x1         0x0         00:00:00:00:00:00     *        wlan0\n"
	if parseArp(lin, "192.168.0.104") != "" {
		t.Error("arp incomplete")
	}
	p, fw, ok := parseProfiles("P|WLAN|Public\r\nP|本地连接* 10|Private\r\nF|Domain|True\r\nF|Private|True\r\nF|Public|True\r\n")
	if !ok || p["WLAN"] != "Public" || p["本地连接* 10"] != "Private" || !strings.Contains(fw, "Public=True") {
		t.Errorf("profiles %v %s", p, fw)
	}
	if classifyDialErr(syscall.Errno(10061)) != "refused" && classifyDialErr(syscall.ECONNREFUSED) != "refused" {
		t.Error("refused")
	}
	if classifyDialErr(errors.New("dial tcp 1.2.3.4:18520: i/o timeout")) != "timeout" || classifyDialErr(nil) != "ok" {
		t.Error("timeout")
	}
}
