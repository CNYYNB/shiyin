package main

import (
	"net"
	"strings"
	"testing"
)

func TestParseHotspot(t *testing.T) {
	st := parseHotspot("junk\r\n{\"ok\":true,\"state\":\"On\",\"ssid\":\"LOUIS-PC 3721\",\"pass\":\"a1b2c3d4\",\"clients\":1,\"maxClients\":8,\"profile\":\"TP-LINK_5G\",\"status\":\"Success\",\"err\":\"\"}\r\n")
	if !st.OK || !st.On || st.SSID != "LOUIS-PC 3721" || st.Pass != "a1b2c3d4" || st.Clients != 1 || st.Error != "" {
		t.Fatalf("on: %+v", st)
	}
	st = parseHotspot(`{"ok":false,"state":"Off","ssid":"X","pass":"y","status":"WiFiDeviceOff","err":""}`)
	if st.OK || st.On || !strings.Contains(st.Error, "Wi-Fi 已关闭") || !strings.Contains(st.Manual, "移动热点") {
		t.Fatalf("wifi off: %+v", st)
	}
	st = parseHotspot(`{"ok":false,"state":"Unknown","status":"NoInternet","err":"no internet connection profile"}`)
	if !strings.Contains(st.Error, "没有连网") {
		t.Fatalf("no internet: %+v", st)
	}
	st = parseHotspot("powershell : 无法将“powershell”项识别为 cmdlet")
	if st.OK || !strings.Contains(st.Error, "无法读取") || st.Manual == "" {
		t.Fatalf("garbage: %+v", st)
	}
}

func TestHotspotFirstWhenOn(t *testing.T) {
	cs := []lanCand{
		{"WLAN", net.ParseIP("192.168.0.103"), adapterInfo{IfType: 71, Gateway: true, Known: true}},
		{"本地连接* 10", net.ParseIP("192.168.137.1"), adapterInfo{IfType: 71, Desc: "Microsoft Wi-Fi Direct Virtual Adapter #2", Known: true}},
	}
	hotspotOn.Store(false)
	if got := rankLanIPs(cs); got[0].IP != "192.168.0.103" {
		t.Fatalf("off: %+v", got)
	}
	hotspotOn.Store(true)
	defer hotspotOn.Store(false)
	if got := rankLanIPs(cs); got[0].IP != "192.168.137.1" || got[0].Label != "电脑热点" {
		t.Fatalf("on: %+v", got)
	}
}
