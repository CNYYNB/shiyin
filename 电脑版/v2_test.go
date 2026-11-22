package main

import (
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"0.2.0", "0.1.0", 1}, {"0.1.0", "0.2.0", -1}, {"0.2.0", "0.2.0", 0}, {"0.10.0", "0.9.9", 1}, {"1.0", "0.99.99", 1}, {"0.2", "0.2.0", 0}, {"v0.3.0", "0.2.9", 1}}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestUnderDir(t *testing.T) {
	home := filepath.FromSlash("/m/拾音")
	if !underDirFold(filepath.FromSlash("/m/拾音/a.mp3"), home) || !underDirFold(filepath.FromSlash("/m/拾音/sub/a.mp3"), home) {
		t.Error("inside not detected")
	}
	if underDirFold(filepath.FromSlash("/m/拾音2/a.mp3"), home) || underDirFold(filepath.FromSlash("/m/a.mp3"), home) {
		t.Error("outside reported inside")
	}
}

func TestBestIndex(t *testing.T) {
	arr := []itunesHit{
		{Title: "Ten Years", Artist: "Eason Chan", Duration: 205000},
		{Title: "十年 (Live)", Artist: "陳奕迅", Album: "演唱會", Duration: 205500},
		{Title: "十年", Artist: "陳奕迅", Album: "黑白灰", Duration: 206000},
	}
	if got := bestIndex(arr, "Ten Years", 205000); got != 2 {
		t.Errorf("bestIndex=%d want 2 (Chinese, not live)", got)
	}
	if got := bestIndex(arr, "Ten Years (Live)", 205000); got != 1 {
		t.Errorf("live wanted: %d", got)
	}
	if got := bestIndex(arr, "Ten Years", 230000); got != -1 {
		t.Errorf("duration too far: %d", got)
	}
}
