package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// A proxy that accepts and immediately drops the connection (what a broken VPN line does).
func resetProxy(t *testing.T) *url.URL {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	u, _ := url.Parse("http://" + ln.Addr().String())
	return u
}

func TestUpdateRouteFallsBackToDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	bad := resetProxy(t)
	viaBad := func(*http.Request) (*url.URL, error) { return bad, nil }
	m := &multiRouteRT{proxyH2: newTransport(viaBad), proxyH1: newTransportH1(viaBad), directH1: newTransportH1(nil), directH2: newTransport(nil), pause: time.Millisecond}
	m.pick = func(*http.Request) []*http.Transport { return []*http.Transport{m.proxyH2, m.proxyH1, m.directH1, m.proxyH2} }
	resp, err := (&http.Client{Transport: m, Timeout: 10 * time.Second}).Get(srv.URL)
	if err != nil {
		t.Fatalf("expected direct fallback, got %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("body %q", b)
	}
}

func TestUpdateRouteAllFail(t *testing.T) {
	bad := resetProxy(t)
	viaBad := func(*http.Request) (*url.URL, error) { return bad, nil }
	m := &multiRouteRT{proxyH2: newTransport(viaBad), proxyH1: newTransportH1(viaBad), pause: time.Millisecond}
	m.pick = func(*http.Request) []*http.Transport { return []*http.Transport{m.proxyH2, m.proxyH1} }
	if _, err := (&http.Client{Transport: m, Timeout: 10 * time.Second}).Get("http://example.invalid/"); err == nil {
		t.Fatal("expected error")
	}
}
