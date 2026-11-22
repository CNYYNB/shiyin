//go:build !windows

package main

import (
	"net"
	"os"
	"strconv"
	"strings"
)

// adapterInfos: Linux/macOS stand-in (tests, dev). Default-gateway interfaces from /proc/net/route.
func adapterInfos() map[string]adapterInfo {
	out := map[string]adapterInfo{}
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) > 2 && f[1] == "00000000" {
			gw := ""
			if v, err := strconv.ParseUint(f[2], 16, 32); err == nil {
				gw = net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
			}
			out[f[0]] = adapterInfo{Gateway: true, GatewayIP: gw, Known: true}
		}
	}
	return out
}
