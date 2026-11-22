package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrackKey(t *testing.T) {
	cases := map[[2]string]string{
		{"Jay Chou - Sunny Day (2).flac", "33300000"}: "jay chou - sunny day|33300000",
		{"Jay Chou - Sunny Day.flac", "33300000"}:     "jay chou - sunny day|33300000",
		{"晴天.MP3", "5"}:                               "晴天|5",
		{" Song (12) .mp3", "7"}:                      "song (12)|7",
		{"Song (a).mp3", "7"}:                         "song (a)|7",
		{"noext", "1"}:                                "noext|1",
	}
	for in, want := range cases {
		var sz int64
		for _, c := range in[1] {
			sz = sz*10 + int64(c-'0')
		}
		if got := trackKey(in[0], sz); got != want {
			t.Errorf("trackKey(%q)=%q want %q", in[0], got, want)
		}
	}
}

func TestDurations(t *testing.T) {
	dir := "test/lib"
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("no fixtures")
	}
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if !audioExts[e.Name()[len(e.Name())-len(filepath.Ext(e.Name()))+1:]] {
			continue
		}
		d := probeDuration(p)
		t.Logf("%s => %d ms", e.Name(), d)
		if d < 11000 || d > 13000 {
			t.Errorf("%s: duration %d, want ~12000", e.Name(), d)
		}
	}
}
