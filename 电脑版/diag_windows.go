//go:build windows

package main

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// runHidden runs a console tool without flashing a window; output decoded from GBK when needed.
func runHidden(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, _ := cmd.CombinedOutput()
	s := string(out)
	if !isUTF8(out) {
		if d, err := simplifiedchinese.GBK.NewDecoder().Bytes(out); err == nil {
			s = string(d)
		}
	}
	return s
}

func isUTF8(b []byte) bool { return strings.ToValidUTF8(string(b), "\uFFFD") == string(b) }

func pingHost(ip string) (bool, string) {
	out := runHidden(8*time.Second, "ping", "-n", "2", "-w", "1000", ip)
	ok := parsePingOK(out)
	return ok, pingSummary(ok, out)
}

func pingSummary(ok bool, out string) string {
	if ok {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(strings.ToUpper(l), "TTL=") {
				return "通：" + strings.TrimSpace(l)
			}
		}
		return "通"
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "无法访问") || strings.Contains(l, "超时") || strings.Contains(strings.ToLower(l), "unreachable") || strings.Contains(strings.ToLower(l), "timed out") {
			return "不通：" + l
		}
	}
	return "不通"
}

func arpLookup(ip string) (string, bool) {
	out := runHidden(5*time.Second, "arp", "-a", ip)
	return parseArp(out, ip), true
}

// netProfiles: adapter alias → Public/Private/DomainAuthenticated, and firewall Enabled per profile.
func netProfiles() (map[string]string, string, bool) {
	ps := `$ErrorActionPreference='SilentlyContinue'; Get-NetConnectionProfile | ForEach-Object { 'P|' + $_.InterfaceAlias + '|' + $_.NetworkCategory }; Get-NetFirewallProfile | ForEach-Object { 'F|' + $_.Name + '|' + $_.Enabled }`
	out := runHidden(12*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", "[Console]::OutputEncoding=[Text.Encoding]::UTF8; "+ps)
	return parseProfiles(out)
}
