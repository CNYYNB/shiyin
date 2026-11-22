package main

// 0.2.2「开启电脑热点」: Windows Mobile Hotspot via WinRT NetworkOperatorTetheringManager (PowerShell helper,
// no admin). For networks with AP/client isolation: the phone joins the PC's hotspot (192.168.137.x).

import (
	"encoding/json"
	"strings"
	"sync/atomic"
)

type HotspotStatus struct {
	OK        bool   `json:"ok"`
	On        bool   `json:"on"`
	State     string `json:"state"` // On | Off | InTransition | Unknown
	SSID      string `json:"ssid"`
	Pass      string `json:"pass"`
	Clients   int    `json:"clients"`
	MaxClient int    `json:"maxClients"`
	Profile   string `json:"profile"` // internet connection shared
	Error     string `json:"error,omitempty"`
	Manual    string `json:"manual"`
	// 0.2.3
	TunWarn     []string `json:"tunWarn,omitempty"`     // active proxy TUN/VPN adapters (asked to confirm first)
	NeedConfirm bool     `json:"needConfirm,omitempty"` // not started: confirm with ?force=1
	Owned       bool     `json:"owned"`                 // turned on by 拾音 (it will turn it off again)
	AutoOff     int      `json:"autoOff,omitempty"`     // seconds after the import finished
}

const hotspotManual = "也可以手动开启：设置 → 网络和 Internet → 移动热点，打开后让手机连上这个热点。"

// hotspotOn caches the last known state so lanIPs() can put the hotspot adapter first.
var hotspotOn atomic.Bool

func hotspotFriendlyErr(status, extra string) string {
	m := map[string]string{
		"MobileBroadbandDeviceOff":   "电脑的网络适配器已关闭",
		"WiFiDeviceOff":              "电脑的 Wi-Fi 已关闭，请先打开 Wi-Fi",
		"EntitlementCheckTimeout":    "检查热点权限超时",
		"EntitlementCheckFailure":    "这台电脑不允许开启热点",
		"OperationInProgress":        "正在开启/关闭中，请稍等几秒再试",
		"BluetoothDeviceOff":         "蓝牙已关闭",
		"NetworkLimitedConnectivity": "电脑当前网络受限，无法共享",
		"NoInternet":                 "电脑现在没有连网，无法共享热点（可以先连上 Wi-Fi 或网线）",
	}
	s := m[status]
	if s == "" {
		s = "开启失败（" + status + "）"
	}
	if extra != "" {
		s += "：" + extra
	}
	return s
}

// parseHotspot reads the helper's JSON line (the last line starting with '{').
func parseHotspot(out string) HotspotStatus {
	var p struct {
		OK         bool   `json:"ok"`
		State      string `json:"state"`
		SSID       string `json:"ssid"`
		Pass       string `json:"pass"`
		Clients    int    `json:"clients"`
		MaxClients int    `json:"maxClients"`
		Profile    string `json:"profile"`
		Status     string `json:"status"`
		Err        string `json:"err"`
	}
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "{") {
			line = l
		}
	}
	st := HotspotStatus{Manual: hotspotManual}
	if line == "" || json.Unmarshal([]byte(line), &p) != nil {
		st.Error = "无法读取电脑热点状态"
		if t := strings.TrimSpace(out); t != "" {
			st.Error += "：" + firstLine(t)
		}
		return st
	}
	st.OK, st.State, st.SSID, st.Pass, st.Clients, st.MaxClient, st.Profile = p.OK, p.State, p.SSID, p.Pass, p.Clients, p.MaxClients, p.Profile
	st.On = p.State == "On"
	if !p.OK {
		if p.Status != "" && p.Status != "Success" {
			st.Error = hotspotFriendlyErr(p.Status, p.Err)
		} else {
			st.Error = "电脑热点操作失败：" + p.Err
		}
	}
	return st
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return strings.TrimSpace(s)
}
