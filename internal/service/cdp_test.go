package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestOptionalCDPDynamicDOM(t *testing.T) {
	endpoint := os.Getenv("TEST_CDP")
	if endpoint == "" {
		t.Skip("set TEST_CDP to exercise an external browser")
	}
	var target *httptest.Server
	external := make(chan bool, 1)
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/external" {
			external <- r.Header.Get("X-Target-Key") == ""
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.Host, "127.0.0.1") && r.Header.Get("X-Target-Key") != "fixture-secret" {
			t.Error("configured CDP header was not sent to its trusted host")
		}
		u, _ := url.Parse(target.URL)
		fmt.Fprintf(w, `<!doctype html><html><body><img src="http://localhost:%s/external"><script>setTimeout(()=>{const e=document.createElement('h1');e.textContent='CDP dynamic fixture';document.body.append(e)},50)</script></body></html>`, u.Port())
	}))
	defer target.Close()
	source := t.TempDir()
	files := communityFiles(t)
	files["scrapers/BrowserFixture.yml"] = []byte("name: Browser Fixture\nsceneByURL:\n - action: scrapeXPath\n   url: [127.0.0.1]\n   scraper: detail\nxPathScrapers:\n detail:\n  scene:\n   Title: //h1\ndriver:\n useCDP: true\n sleep: 1\n")
	writeFiles(t, source, files)
	a, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := a.Config()
	c.LocalDir = source
	c.Update.Enabled = false
	c.CDP = endpoint
	c.AllowPrivateTargets = true
	c.SiteIntervalMS = 0
	c.Scrapers["BrowserFixture"] = ScraperConfig{Enabled: true, Headers: map[string]string{"X-Target-Key": "fixture-secret"}}
	if e = a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	if e = a.InstallLocal(context.Background()); e != nil {
		t.Fatal(e)
	}
	result, e := a.Scrape(context.Background(), ScrapeRequest{ScraperID: "BrowserFixture", Object: "scene", URL: target.URL}, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Results) != 1 || result.Results[0].Title == nil || *result.Results[0].Title != "CDP dynamic fixture" {
		t.Fatal("browser JavaScript was not rendered", result)
	}
	if !<-external {
		t.Fatal("CDP forwarded a configured secret to an untrusted subresource host")
	}
}
