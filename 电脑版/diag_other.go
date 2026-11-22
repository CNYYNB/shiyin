//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

func pingHost(ip string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ping", "-c", "2", "-W", "1", ip).CombinedOutput()
	if err != nil && len(out) == 0 {
		return false, "无法运行 ping：" + err.Error()
	}
	ok := parsePingOK(string(out))
	if ok {
		return true, "通"
	}
	return false, "不通"
}

func arpLookup(ip string) (string, bool) {
	b, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return "", false
	}
	return parseArp(string(b), ip), true
}

func netProfiles() (map[string]string, string, bool) { return nil, "", false }

var _ = strings.TrimSpace
