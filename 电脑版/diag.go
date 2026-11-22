package main

// 0.2.2「网络诊断」: why can't this PC and the phone reach each other? Produces a short Chinese report
// (copyable) that ends with a one-line verdict. Probes: local adapters, the route Windows picks for the
// phone IP, ping gateway + phone, ARP entry for the phone, TCP connect to phone:18520 with the exact
// error class, the reverse listener, and the Windows network profile + firewall state.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

type diagAdapter struct {
	Name, IP, Net, Gateway, Kind string
	Virtual                      bool
}

// diagFacts are the probe results the verdict is computed from (pure → unit-tested).
type diagFacts struct {
	PhoneIP       string
	SameSubnet    bool   // some adapter's subnet contains the phone IP
	RouteLocal    string // local address the OS picks for the phone IP
	RouteVirtual  bool   // that address belongs to a TUN/VPN/virtual adapter or isn't on the phone's subnet
	GatewayIP     string
	GatewayPing   int // 1 ok, 0 fail, -1 not tried
	PhonePing     int
	PingNoHost    bool   // ping answered by our own IP: "无法访问目标主机" / "Destination host unreachable" (ARP failed)
	ArpOK         int    // 1 phone MAC known, 0 none, -1 unknown
	TCP           string // ok | refused | timeout | unreachable | error
	TCPErr        string
	Listening     bool
	PublicFW      bool // the adapter on the phone's subnet is "Public" and the Public firewall is on
	ProfileKnown  bool
	HotspotSubnet bool // phone is on the PC's own Windows hotspot (192.168.137.x)
}

func classifyDialErr(err error) string {
	if err == nil {
		return "ok"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	var en syscall.Errno
	if errors.As(err, &en) {
		switch uintptr(en) {
		case 10061, uintptr(syscall.ECONNREFUSED):
			return "refused"
		case 10060, uintptr(syscall.ETIMEDOUT):
			return "timeout"
		case 10065, 10051, uintptr(syscall.EHOSTUNREACH), uintptr(syscall.ENETUNREACH):
			return "unreachable"
		}
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "refused") || strings.Contains(m, "积极拒绝"):
		return "refused"
	case strings.Contains(m, "timeout") || strings.Contains(m, "timed out") || strings.Contains(m, "没有正确答复"):
		return "timeout"
	case strings.Contains(m, "unreachable") || strings.Contains(m, "无法访问"):
		return "unreachable"
	}
	return "error"
}

var tcpWords = map[string]string{"ok": "连接成功", "refused": "被拒绝（对方没在监听这个端口）", "timeout": "超时（数据包被丢弃）",
	"unreachable": "网络不可达", "error": "出错"}

// isolationAdvice: 0.2.3 recommended order when the router isolates Wi-Fi clients. The PC hotspot is last:
// on some laptops (Intel AX201) it froze the whole PC during a large transfer.
const isolationAdvice = "建议按顺序换一种方式：① 手机开热点，电脑连上手机的热点（只在两台设备之间传，不耗手机流量，保险起见可先关掉手机的移动数据）；" +
	"② 用数据线把手机的 Music/拾音 文件夹复制到电脑的拾音文件夹；③ 登录路由器关闭 AP 隔离；④ 电脑开热点（不推荐，部分笔记本无线网卡驱动可能导致电脑死机）"

// diagIsolation: the router (not the phone or this PC) blocks Wi-Fi clients from reaching each other.
func diagIsolation(f diagFacts) bool {
	return f.SameSubnet && !f.RouteVirtual && f.TCP != "ok" && f.TCP != "refused" &&
		f.GatewayPing == 1 && f.PhonePing != 1 && (f.ArpOK >= 0 || f.PingNoHost)
}

// diagVerdict turns the facts into one line (plus an optional second hint about the firewall).
func diagVerdict(f diagFacts) string {
	var v string
	switch {
	case !f.SameSubnet:
		v = "电脑没有和手机同网段的网卡 → 两台设备不在同一个网络（检查是否连的同一个 Wi-Fi，或手机 IP 填错）"
	case f.RouteVirtual:
		v = "去手机的流量走了 VPN/虚拟网卡路由 → 关掉电脑 VPN/TUN（或设为局域网直连）再试"
	case f.TCP == "ok":
		v = "电脑能连上手机的服务，网络是通的 → 在「从手机导入」里输入手机 IP 和配对码即可"
	case f.TCP == "refused":
		v = "手机能访问，但 18520 端口拒绝连接 → 手机服务未运行（手机上打开「设置 → 传到电脑」页面）"
	case diagIsolation(f):
		why := "能连网关，但连不上手机，且手机 ARP 正常"
		if f.ArpOK == 0 || f.PingNoHost {
			why = "能连网关，但电脑解析不到手机的 MAC（ARP 失败" + map[bool]string{true: "，ping 报“无法访问目标主机”", false: ""}[f.PingNoHost] + "）"
		}
		v = why + " → 路由器开了 AP 隔离（也叫客户端隔离；或者手机连的是访客 Wi-Fi），同一个 Wi-Fi 下的设备互相访问不了。" + isolationAdvice
	case f.PhonePing == 1 && f.TCP == "timeout":
		v = "能 ping 通手机但端口超时 → 手机拦截了入站连接（手机 VPN / 系统省电冻结）"
	case f.GatewayPing == 0:
		v = "电脑连网关都不通 → 电脑这边的网络有问题"
	default:
		v = "手机不可达（" + tcpWords[f.TCP] + "）→ 很可能是路由器开了 AP 隔离。" + isolationAdvice
	}
	if f.PublicFW && !f.HotspotSubnet {
		v += "；另外电脑当前网络是“公用网络”且防火墙开启，「手机连电脑」会被拦截 → 改成专用网络或在防火墙里允许 拾音"
	} else if f.PublicFW {
		v += "；电脑热点网络是“公用网络”且防火墙开启，「手机连电脑」会被拦截（电脑连手机不受影响）"
	}
	return "结论：" + v
}

// runDiag returns the report and whether AP/client isolation was detected.
func runDiag(phoneIP string) (string, bool) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	host, _ := os.Hostname()
	w("拾音电脑版 %s 网络诊断 · %s · %s", appVersion, host, time.Now().Format("2006-01-02 15:04:05"))
	f := diagFacts{PhoneIP: phoneIP, GatewayPing: -1, PhonePing: -1, ArpOK: -1}
	pip := net.ParseIP(strings.TrimSpace(phoneIP)).To4()

	// adapters
	infos := adapterInfos()
	w("【网卡】")
	ifs, _ := net.Interfaces()
	var phoneIfc string
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		ai := infos[ifc.Name]
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ones, _ := ipn.Mask.Size()
			kind := lanLabels[lanKind(ifc.Name, ipn.IP, ai)]
			flags := []string{}
			if kind != "" {
				flags = append(flags, kind)
			}
			if virtualIface(ifc.Name) || (ai.Desc != "" && virtualIface(ai.Desc) && !strings.Contains(strings.ToLower(ai.Desc), "wi-fi direct")) || skipLanIP(ipn.IP) {
				flags = append(flags, "VPN/虚拟")
			}
			gw := ai.GatewayIP
			if gw == "" {
				gw = "无"
			}
			mark := ""
			if pip != nil && ipn.Contains(pip) {
				mark = " ← 与手机同网段"
				f.SameSubnet = true
				phoneIfc = ifc.Name
				if ai.GatewayIP != "" {
					f.GatewayIP = ai.GatewayIP
				}
				if ipn.IP.To4()[0] == 192 && ipn.IP.To4()[1] == 168 && ipn.IP.To4()[2] == 137 {
					f.HotspotSubnet = true
				}
			}
			w("- %s %s/%d 网关 %s [%s]%s", ifc.Name, ipn.IP.To4(), ones, gw, strings.Join(flags, ","), mark)
		}
	}
	if pip == nil {
		w("手机 IP「%s」格式不对，只检查了本机。", phoneIP)
		w("%s", diagListener(&f))
		return b.String(), false
	}

	// route the OS picks (a UDP "connect" sends nothing but resolves the route + source address)
	if c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: pip, Port: 9}); err == nil {
		la := c.LocalAddr().(*net.UDPAddr).IP
		c.Close()
		f.RouteLocal = la.String()
		name := ""
		for _, ifc := range ifs {
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(la) {
					name = ifc.Name
					f.RouteVirtual = !ipn.Contains(pip) || virtualIface(ifc.Name) || skipLanIP(la)
				}
			}
		}
		w("【路由】去 %s 走本机 %s（%s）%s", pip, la, name, map[bool]string{true: " ⚠ 不是同网段网卡", false: ""}[f.RouteVirtual])
	} else {
		w("【路由】查不到去 %s 的路由：%v", pip, err)
	}

	// ping
	if f.GatewayIP != "" {
		ok, line := pingHost(f.GatewayIP)
		f.GatewayPing = b2i(ok)
		w("【ping 网关 %s】%s", f.GatewayIP, line)
	}
	ok, line := pingHost(pip.String())
	f.PhonePing = b2i(ok)
	f.PingNoHost = !ok && pingNoHost(line)
	w("【ping 手机 %s】%s", pip, line)
	if mac, known := arpLookup(pip.String()); known {
		if mac != "" {
			f.ArpOK = 1
			w("【ARP】手机 MAC %s（局域网里能找到手机）", mac)
		} else {
			f.ArpOK = 0
			w("【ARP】没有手机的 ARP 记录（局域网里找不到手机）")
		}
	}

	// TCP to the phone's server
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	c, err := lanDial(ctx, "tcp", net.JoinHostPort(pip.String(), "18520"))
	cancel()
	f.TCP = classifyDialErr(err)
	if c != nil {
		c.Close()
	}
	if err != nil {
		f.TCPErr = err.Error()
		w("【TCP %s:18520】%s，%d ms：%s", pip, tcpWords[f.TCP], time.Since(start).Milliseconds(), err.Error())
	} else {
		w("【TCP %s:18520】%s，%d ms", pip, tcpWords[f.TCP], time.Since(start).Milliseconds())
	}

	w("%s", diagListener(&f))

	// Windows profile + firewall
	if profiles, fw, okp := netProfiles(); okp {
		f.ProfileKnown = true
		pp := []string{}
		for k, v := range profiles {
			pp = append(pp, k+"="+v)
		}
		w("【网络类型】%s；防火墙 %s", strings.Join(pp, "，"), fw)
		if phoneIfc != "" && strings.EqualFold(profiles[phoneIfc], "Public") && strings.Contains(fw, "Public=True") {
			f.PublicFW = true
		}
	}
	w("%s", diagVerdict(f))
	if diagIsolation(f) {
		w("%s", netRepairHint)
	}
	return b.String(), diagIsolation(f)
}

// netRepairHint (0.2.3): after a crash while the PC hotspot was on, Windows networking / the proxy may stay broken.
const netRepairHint = "【网络修复】如果电脑开过热点后死机，重启后代理或网络一直超时：① 控制面板 → 网络连接，禁用“本地连接*”" +
	"（Microsoft Wi-Fi Direct Virtual Adapter）；② 以管理员身份打开终端，运行 netsh winsock reset 和 netsh int ip reset（出现一行“拒绝访问”是正常的）；③ 重启电脑。"

func diagListener(f *diagFacts) string {
	st := rev.status()
	if st.Listening {
		f.Listening = true
		ips := []string{}
		for _, x := range st.IPs {
			ips = append(ips, x.IP+"("+strings.TrimSpace(x.Label+" "+x.Iface)+")")
		}
		return fmt.Sprintf("【手机连电脑】正在监听 TCP %d（所有网卡），UDP 搜索 %v；手机可用：%s", st.Port, st.UDP, strings.Join(ips, "，"))
	}
	return "【手机连电脑】未在监听（打开「从手机导入」窗口时才会开启）"
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parsePingOK: a reply line contains TTL= (Windows "ping" exits 0 even for "目标主机无法访问").
func parsePingOK(out string) bool {
	return strings.Contains(strings.ToUpper(out), "TTL=")
}

// parseArp finds the MAC for ip in `arp -a` (Windows) or /proc/net/arp output; "" if absent/incomplete.
func parseArp(out, ip string) string {
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || f[0] != ip {
			continue
		}
		for _, x := range f[1:] {
			x = strings.ToLower(x)
			if (strings.Count(x, "-") == 5 || strings.Count(x, ":") == 5) && x != "00:00:00:00:00:00" && x != "00-00-00-00-00-00" && x != "ff-ff-ff-ff-ff-ff" {
				return x
			}
		}
	}
	return ""
}

// parseProfiles reads the "P|alias|category" / "F|name|enabled" lines from the PowerShell probe.
func parseProfiles(out string) (map[string]string, string, bool) {
	prof := map[string]string{}
	fw := []string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(l), "|")
		if len(f) != 3 {
			continue
		}
		switch f[0] {
		case "P":
			prof[f[1]] = f[2]
		case "F":
			fw = append(fw, f[1]+"="+f[2])
		}
	}
	return prof, strings.Join(fw, " "), len(prof) > 0 || len(fw) > 0
}

// pingNoHost: Windows answers from its own address with 无法访问目标主机 when ARP for the target fails.
func pingNoHost(out string) bool {
	l := strings.ToLower(out)
	return strings.Contains(out, "无法访问目标主机") || strings.Contains(l, "destination host unreachable")
}
