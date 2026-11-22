package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSkipLanIP(t *testing.T) {
	for ip, skip := range map[string]bool{
		"192.168.0.103": false, "10.1.2.3": false, "172.20.1.1": false,
		"198.18.0.1": true, "198.19.255.1": true, "169.254.10.2": true, "127.0.0.1": true, "100.100.1.1": true,
	} {
		if got := skipLanIP(net.ParseIP(ip)); got != skip {
			t.Errorf("skipLanIP(%s) = %v, want %v", ip, got, skip)
		}
	}
	for name, virt := range map[string]bool{"vpn07": true, "WLAN": false, "以太网": false, "Meta": true, "vEthernet (WSL)": true, "Clash": true, "wintun": true, "Wi-Fi": false, "VMware Network Adapter VMnet8": true} {
		if got := virtualIface(name); got != virt {
			t.Errorf("virtualIface(%q) = %v, want %v", name, got, virt)
		}
	}
}

// fakeReversePhone dials the hub like the Android app: handshake, then serve one HTTP request.
func fakeReversePhone(t *testing.T, port int, code string, serve bool) string {
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(c, "SHIYIN-REVERSE/1 %s {\"name\":\"Fake\",\"version\":\"2.2\",\"count\":3}\n", code)
	r := bufio.NewReader(c)
	line, _ := r.ReadString('\n')
	if !strings.HasPrefix(line, "OK") || !serve {
		c.Close()
		return strings.TrimSpace(line)
	}
	go func() {
		defer c.Close()
		var req string
		for { // skip keep-alive blank lines
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimSpace(l) != "" {
				req = l
				break
			}
		}
		for { // headers
			l, err := r.ReadString('\n')
			if err != nil || strings.TrimSpace(l) == "" {
				break
			}
		}
		body := "pong " + strings.Fields(req)[1]
		fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	}()
	return strings.TrimSpace(line)
}

func TestReverseHub(t *testing.T) {
	os.Setenv("SHIYIN_REVERSE_PORT", "28522")
	os.Setenv("SHIYIN_REVERSE_UDP", "28523")
	defer os.Unsetenv("SHIYIN_REVERSE_PORT")
	defer os.Unsetenv("SHIYIN_REVERSE_UDP")
	st, err := rev.start()
	if err != nil {
		t.Fatal(err)
	}
	defer rev.stop()
	if len(st.Code) != 6 || st.Port != 28522 {
		t.Fatalf("bad status %+v", st)
	}
	if got := fakeReversePhone(t, st.Port, "999999", false); !strings.HasPrefix(got, "ERR badcode") {
		t.Fatalf("wrong code accepted: %q", got)
	}
	// UDP discovery
	u, _ := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 28523})
	u.Write([]byte("SHIYIN_FIND_PC"))
	u.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, err := u.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), `"app":"shiyin-pc"`) || !strings.Contains(string(buf[:n]), `"port":28522`) {
		t.Fatalf("udp discovery: %v %s", err, buf[:n])
	}
	u.Close()

	for i := 0; i < 2; i++ {
		if got := fakeReversePhone(t, st.Port, st.Code, true); !strings.HasPrefix(got, "OK ") {
			t.Fatalf("handshake: %q", got)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if s := rev.heartbeat(); s.Phone == nil || s.Phone.Name != "Fake" || s.Conns != 2 {
		t.Fatalf("status %+v", s)
	}
	for _, p := range []string{"/api/ping", "/api/library"} {
		resp, err := reverseClient.Get("http://shiyin-phone" + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != "pong "+p {
			t.Fatalf("got %q", b)
		}
	}
	// pool empty → a request waits for the phone to dial in
	go func() { time.Sleep(300 * time.Millisecond); fakeReversePhone(t, st.Port, st.Code, true) }()
	resp, err := reverseClient.Get("http://shiyin-phone/late")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

// TestInteropWithAndroid (manual): SHIYIN_INTEROP=/tmp/hub go test -run Interop .
// Starts the real hub, writes "port code" to $SHIYIN_INTEROP.txt, waits for the real Kotlin
// ReverseClient (app unit test InteropTest) to connect, then drives /api/ping + /api/library over it.
func TestInteropWithAndroid(t *testing.T) {
	base := os.Getenv("SHIYIN_INTEROP")
	if base == "" {
		t.Skip("set SHIYIN_INTEROP to run against the Android ReverseClient")
	}
	os.Setenv("SHIYIN_REVERSE_PORT", "38522")
	os.Setenv("SHIYIN_REVERSE_UDP", "38523")
	st, err := rev.start()
	if err != nil {
		t.Fatal(err)
	}
	defer rev.stop()
	os.WriteFile(base+".txt", []byte(fmt.Sprintf("%d %s", st.Port, st.Code)), 0o644)
	deadline := time.Now().Add(150 * time.Second)
	for rev.heartbeat().Phone == nil {
		if time.Now().After(deadline) {
			t.Fatal("phone never connected")
		}
		time.Sleep(200 * time.Millisecond)
	}
	var out []string
	for _, p := range []string{"/api/ping", "/api/library", "/api/file?key=nope"} {
		resp, err := reverseClient.Get("http://shiyin-phone" + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		out = append(out, fmt.Sprintf("%s %d %s", p, resp.StatusCode, b))
	}
	os.WriteFile(base+".result", []byte(strings.Join(out, "\n")), 0o644)
	t.Log(strings.Join(out, "\n"))
	if !strings.Contains(out[0], `"app":"shiyin"`) || !strings.Contains(out[1], `"tracks":[`) || !strings.HasPrefix(out[2], "/api/file?key=nope 404") {
		t.Fatal("unexpected responses")
	}
}

func TestRankLanIPs(t *testing.T) {
	ip := net.ParseIP
	// user's PC: Windows Mobile Hotspot adapter listed before the real Wi-Fi
	got := rankLanIPs([]lanCand{
		{"本地连接* 10", ip("192.168.137.1"), adapterInfo{IfType: 71, Desc: "Microsoft Wi-Fi Direct Virtual Adapter #2", Known: true}},
		{"以太网 2", ip("10.0.5.2"), adapterInfo{IfType: 6, Known: true}},
		{"WLAN", ip("192.168.0.103"), adapterInfo{IfType: 71, Gateway: true, Desc: "Intel(R) Wi-Fi 6 AX201", Known: true}},
	})
	want := []string{"192.168.0.103", "10.0.5.2", "192.168.137.1"}
	for i, w := range want {
		if got[i].IP != w {
			t.Fatalf("order = %+v, want %v", got, want)
		}
	}
	if got[0].Label != "Wi-Fi" || !got[0].GW || got[1].Label != "有线" || got[2].Label != "电脑热点" || got[2].Kind != "hotspot" {
		t.Errorf("labels: %+v", got)
	}
	// wired with gateway beats Wi-Fi without
	got = rankLanIPs([]lanCand{
		{"WLAN", ip("192.168.1.5"), adapterInfo{IfType: 71, Known: true}},
		{"以太网", ip("192.168.0.20"), adapterInfo{IfType: 6, Gateway: true, Known: true}},
	})
	if got[0].IP != "192.168.0.20" || got[0].Kind != "eth" {
		t.Errorf("gateway first: %+v", got)
	}
	// no gateway info (non-Windows): name-based order, hotspot range still last
	got = rankLanIPs([]lanCand{
		{"x", ip("192.168.137.1"), adapterInfo{}},
		{"eth0", ip("172.16.1.2"), adapterInfo{}},
		{"wlan0", ip("192.168.0.9"), adapterInfo{}},
	})
	if got[0].IP != "192.168.0.9" || got[1].IP != "172.16.1.2" || got[2].Label != "电脑热点" {
		t.Errorf("no-info order: %+v", got)
	}
	// only the hotspot: still offered
	got = rankLanIPs([]lanCand{{"本地连接* 10", ip("192.168.137.1"), adapterInfo{}}})
	if len(got) != 1 || got[0].Kind != "hotspot" {
		t.Errorf("hotspot only: %+v", got)
	}
}
