package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func TestReverseProxyHostAndOrigin(t *testing.T) {
	t.Setenv("STASHSCRAPERS_TOKEN", "reverse-proxy-fixture-token")
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(a.Handler())
	defer backend.Close()
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(a.Config())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, host, origin, upstreamHost, forwardedHost string
		status                                          int
	}{
		{"preserved HTTPS domain", "scrapers.example", "https://scrapers.example", "", "", http.StatusOK},
		{"preserved custom port", "scrapers.example:8443", "https://scrapers.example:8443", "", "", http.StatusOK},
		{"rewritten upstream Host", "scrapers.example", "https://scrapers.example", "127.0.0.1:8080", "", http.StatusForbidden},
		{"forwarded Host cannot replace Host", "scrapers.example", "https://scrapers.example", "127.0.0.1:8080", "scrapers.example", http.StatusForbidden},
		{"dropped custom port", "scrapers.example:8443", "https://scrapers.example:8443", "scrapers.example", "", http.StatusForbidden},
		{"foreign browser origin", "scrapers.example", "https://foreign.example", "", "foreign.example", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := httputil.NewSingleHostReverseProxy(target)
			director := proxy.Director
			proxy.Director = func(r *http.Request) {
				director(r)
				if tc.upstreamHost != "" {
					r.Host = tc.upstreamHost
				}
				r.Header.Set("X-Forwarded-Proto", "https")
				r.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			}
			front := httptest.NewTLSServer(proxy)
			defer front.Close()
			r, err := http.NewRequest(http.MethodPut, front.URL+"/api/config", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Authorization", "Bearer reverse-proxy-fixture-token")
			r.Header.Set("Content-Type", "application/json")
			response, err := front.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("got HTTP %d, want %d", response.StatusCode, tc.status)
			}
		})
	}
}
