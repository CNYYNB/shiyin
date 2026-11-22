//go:build !windows

package main

import "os"

// Non-Windows (tests): SHIYIN_FAKE_HOTSPOT=<json printed by the PowerShell helper> stands in for it.
func hotspotDo(action string) HotspotStatus {
	if f := os.Getenv("SHIYIN_FAKE_HOTSPOT"); f != "" {
		st := parseHotspot(f)
		if action == "on" && st.OK {
			st.On, st.State = true, "On"
		}
		if action == "off" && st.OK {
			st.On, st.State = false, "Off"
		}
		hotspotOn.Store(st.On)
		return st
	}
	return HotspotStatus{Error: "只有 Windows 版支持一键开启热点", Manual: hotspotManual}
}
