package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"stashscrapers/internal/engine"
)

func TestPinnedCommunityRegression(t *testing.T) {
	if e := runRegression(context.Background(), &Snapshot{Entries: map[string]*Entry{}}); e != nil {
		t.Fatal(e)
	}
}
func writeFiles(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for name, b := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
func communityFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"AdultEmpire.yml", "1pondo.yml", "Boobpedia.yml", "Anime-DB.yml"} {
		b, e := fixtures.ReadFile("testdata/community/" + name)
		if e != nil {
			t.Fatal(e)
		}
		files["scrapers/"+name] = b
	}
	return files
}
func fixtureApp(t *testing.T) *App {
	t.Helper()
	source := t.TempDir()
	writeFiles(t, source, communityFiles(t))
	a, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := a.Config()
	c.LocalDir = source
	c.AllowPrivateTargets = true
	c.SiteIntervalMS = 0
	c.Update.Enabled = false
	if e = a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	if e = a.InstallLocal(context.Background()); e != nil {
		t.Fatal(e)
	}
	return a
}

type routeTransport struct {
	server *url.URL
	base   http.RoundTripper
}

func (t routeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("X-Fixture-Original-URL", r.URL.String())
	u := *r.URL
	u.Scheme = t.server.Scheme
	u.Host = t.server.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}
func controlledTarget(t *testing.T, a *App) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "movie_details/"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, fixtureBody("1pondo.json"))
		case r.URL.Path == "/wiki/api.php":
			fmt.Fprint(w, fixtureBody("boobpedia-search.json"))
		case r.URL.Path == "/boobs/File:Portrait.jpg":
			fmt.Fprint(w, fixtureBody("boobpedia-image.html"))
		case strings.Contains(r.URL.Path, "/boobs/"):
			fmt.Fprint(w, fixtureBody("boobpedia-detail.html"))
		case strings.Contains(r.URL.Path, "/vod/search"):
			if r.URL.Query().Get("q") != "fixture query" {
				t.Errorf("query escaping failed: %s", r.URL.RawQuery)
			}
			if !strings.Contains(r.Header.Get("Cookie"), "ageConfirmed=true") {
				t.Error("upstream cookie not sent")
			}
			fmt.Fprint(w, fixtureBody("empire-search.html"))
		default:
			body := fixtureBody("empire-detail.html")
			if strings.Contains(r.URL.Path, "10002") {
				body = strings.ReplaceAll(body, "Fixture One", "Fixture Two")
				body = strings.ReplaceAll(body, "10001/fixture-one", "10002/fixture-two")
			}
			fmt.Fprint(w, body)
		}
	}))
	u, _ := url.Parse(server.URL)
	a.targetTransport = routeTransport{u, http.DefaultTransport}
	t.Cleanup(server.Close)
	return server
}
func callAPI(t *testing.T, h http.Handler, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "http://localhost"+path, bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]interface{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e, w.Body.String())
	}
	return w.Code, out
}
func TestRealCommunityHTTPAndCandidateFlow(t *testing.T) {
	a := fixtureApp(t)
	controlledTarget(t, a)
	h := a.Handler()
	code, out := callAPI(t, h, "/api/search", ScrapeRequest{ScraperID: "AdultEmpire", Object: "scene", Query: "fixture query"})
	if code != 200 {
		t.Fatal(code, out)
	}
	list := out["results"].([]interface{})
	if len(list) != 2 {
		t.Fatal(out)
	}
	selected := list[1].(map[string]interface{})
	code, detail := callAPI(t, h, "/api/detail", ScrapeRequest{ScraperID: "AdultEmpire", Object: "scene", Operation: "query_fragment", Fragment: selected["raw"].(map[string]interface{}), RulesVersion: out["rules_version"].(string)})
	if code != 200 {
		t.Fatal(code, detail)
	}
	one := detail["results"].([]interface{})[0].(map[string]interface{})
	if one["title"] != "Fixture Two" || one["code"] != nil {
		t.Fatal("wrong candidate or fabricated metadata", one)
	}
	for _, req := range []ScrapeRequest{{ScraperID: "1pondo", Object: "scene", URL: "https://en.1pondo.tv/movies/010124_001/"}, {ScraperID: "Boobpedia", Object: "performer", URL: "https://www.boobpedia.com/boobs/Fixture_Person"}} {
		code, data := callAPI(t, h, "/api/detail", req)
		if code != 200 {
			t.Fatal(code, data)
		}
	}
	code, unsupported := callAPI(t, h, "/api/search", ScrapeRequest{ScraperID: "1pondo", Object: "scene", Query: "010124_001"})
	if code != 422 || unsupported["error"].(map[string]interface{})["code"] != "unsupported" {
		t.Fatal(code, unsupported)
	}
	code, invalidFragment := callAPI(t, h, "/api/detail", ScrapeRequest{ScraperID: "1pondo", Object: "scene", Fragment: map[string]interface{}{"title": "missing filename"}})
	if code != 400 || invalidFragment["error"].(map[string]interface{})["code"] != "input_error" {
		t.Fatal(code, invalidFragment)
	}
	code, missing := callAPI(t, h, "/api/detail", ScrapeRequest{ScraperID: "Anime-DB", Object: "scene", URL: "https://adultanime.dbsearch.net/item/123"})
	if code != 424 || missing["error"].(map[string]interface{})["code"] != "missing_dependency" {
		t.Fatal(code, missing)
	}
}

type mockUpstream struct {
	mu                sync.Mutex
	sha               string
	archives          map[string][]byte
	downloads, checks int
	truncated         bool
}

func archiveBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, b := range files {
		if e := tw.WriteHeader(&tar.Header{Name: "repository/" + name, Mode: 0600, Size: int64(len(b))}); e != nil {
			t.Fatal(e)
		}
		_, _ = tw.Write(b)
	}
	_ = tw.Close()
	_ = gz.Close()
	return out.Bytes()
}
func newMock(t *testing.T) (*mockUpstream, *httptest.Server) {
	t.Helper()
	m := &mockUpstream{archives: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if strings.Contains(r.URL.Path, "/tar.gz/") {
			m.downloads++
			b := m.archives[pathLast(r.URL.Path)]
			if m.truncated {
				b = b[:len(b)/2]
			}
			_, _ = w.Write(b)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/commits/") {
			m.checks++
			if !strings.HasSuffix(r.URL.Path, "/actual-default") {
				http.Error(w, "branch is wrong", 400)
				return
			}
			etag := `"` + m.sha + `"`
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(304)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": m.sha})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"default_branch": "actual-default"})
	}))
	t.Cleanup(server.Close)
	return m, server
}
func pathLast(s string) string { parts := strings.Split(s, "/"); return parts[len(parts)-1] }
func (m *mockUpstream) publish(t *testing.T, sha string, files map[string][]byte) {
	b := archiveBytes(t, files)
	m.mu.Lock()
	m.sha = sha
	m.archives[sha] = b
	m.mu.Unlock()
}
func mockApp(t *testing.T, server *httptest.Server) *App {
	t.Helper()
	a, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := a.Config()
	c.Source.APIBase = server.URL
	c.Source.ArchiveBase = server.URL
	c.Source.Repository = "test/community"
	c.AllowPrivateTargets = true
	c.SiteIntervalMS = 0
	if e = a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	return a
}

const v1 = "1111111111111111111111111111111111111111"
const v2 = "2222222222222222222222222222222222222222"
const v3 = "3333333333333333333333333333333333333333"
const v4 = "4444444444444444444444444444444444444444"

func TestUpdaterSchedulePersistenceSyncRollbackAndIsolation(t *testing.T) {
	m, server := newMock(t)
	files := communityFiles(t)
	files["scrapers/shared/resource.txt"] = []byte("v1")
	m.publish(t, v1, files)
	a := mockApp(t, server)
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if a.State().Active != v1 || a.State().Branch != "actual-default" {
		t.Fatal(a.State())
	}
	next := a.State().NextCheck
	if d := next.Sub(a.State().LastCheck); d != 24*time.Hour {
		t.Fatalf("default is not daily: %v", d)
	}
	c := a.Config()
	c.Scrapers["AdultEmpire"] = ScraperConfig{Enabled: true, Cookie: "private-cookie", APIKey: "private-key"}
	if e := a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	restart, e := New(a.Data)
	if e != nil {
		t.Fatal(e)
	}
	if !restart.State().NextCheck.Equal(next) {
		t.Fatal("restart discarded schedule")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go restart.RunScheduler(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	m.mu.Lock()
	checks := m.checks
	m.mu.Unlock()
	if checks != 1 {
		t.Fatalf("restart rechecked upstream: %d", checks)
	}
	old, release := a.acquire()
	defer release()
	files2 := communityFiles(t)
	files2["scrapers/EmpireRenamed.yml"] = files2["scrapers/AdultEmpire.yml"]
	delete(files2, "scrapers/AdultEmpire.yml")
	files2["scrapers/shared/resource.txt"] = []byte("v2")
	m.publish(t, v2, files2)
	if e = a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	fresh := a.current.Load()
	if _, exists := fresh.Entries["AdultEmpire"]; exists {
		t.Fatal("deleted definition survived")
	}
	if _, exists := fresh.Entries["EmpireRenamed"]; !exists {
		t.Fatal("renamed definition not discovered")
	}
	b, _ := os.ReadFile(filepath.Join(old.Root, "scrapers/shared/resource.txt"))
	if string(b) != "v1" {
		t.Fatal("active reader files were overwritten")
	}
	b, _ = os.ReadFile(filepath.Join(fresh.Root, "scrapers/shared/resource.txt"))
	if string(b) != "v2" {
		t.Fatal("new snapshot wrong")
	}
	if a.Config().Scrapers["AdultEmpire"].Cookie != "private-cookie" || !a.Config().Scrapers["AdultEmpire"].Enabled {
		t.Fatal("user settings were overwritten")
	}
	tombstone := false
	for _, e := range a.List() {
		if e.ID == "AdultEmpire" {
			tombstone = e.State == "unsupported" && strings.Contains(e.Reason, "removed")
		}
	}
	if !tombstone {
		t.Fatal("removed scraper not diagnosed")
	}
	if e = a.Rollback(context.Background(), v1); e != nil {
		t.Fatal(e)
	}
	if e = a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if a.State().Active != v1 || a.Config().Update.Pin != v1 {
		t.Fatal("rollback immediately reinstalled upstream version")
	}
	m.mu.Lock()
	downloads := m.downloads
	m.mu.Unlock()
	if downloads != 2 {
		t.Fatalf("unnecessary downloads: %d", downloads)
	}
	// Offline restart uses only installed resources, independent of source availability.
	server.Close()
	again, e := New(a.Data)
	if e != nil || again.State().Active != v1 {
		t.Fatal("offline restart failed", e)
	}
}
func TestUpdaterInterruptedInvalidAndFailedCommitSuppression(t *testing.T) {
	m, server := newMock(t)
	files := communityFiles(t)
	m.publish(t, v1, files)
	a := mockApp(t, server)
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	m.publish(t, v2, files)
	m.mu.Lock()
	m.truncated = true
	m.mu.Unlock()
	if e := a.Check(context.Background()); e == nil {
		t.Fatal("truncated download accepted")
	}
	if a.State().Active != v1 {
		t.Fatal("download failure switched version")
	}
	bad := communityFiles(t)
	bad["scrapers/AdultEmpire.yml"] = []byte("name: AdultEmpire\nsceneByURL:\n - action: unavailable-new-engine-action\n   url: [adultempire.com]\n")
	m.mu.Lock()
	m.truncated = false
	m.mu.Unlock()
	m.publish(t, v3, bad)
	if e := a.Check(context.Background()); e == nil {
		t.Fatal("incompatible update accepted")
	}
	if a.State().Active != v1 || a.State().Failed[v3] == "" {
		t.Fatal("validation did not preserve version")
	}
	m.mu.Lock()
	before := m.downloads
	m.mu.Unlock()
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	after := m.downloads
	m.mu.Unlock()
	if before != after {
		t.Fatal("failed commit was downloaded again")
	}
	m.publish(t, v4, files)
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if a.State().Active != v4 {
		t.Fatal("later fix was not followed")
	}
}
func TestInternalSchedulerAppliesPublishedCommit(t *testing.T) {
	m, server := newMock(t)
	files := communityFiles(t)
	m.publish(t, v1, files)
	a := mockApp(t, server)
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	m.publish(t, v2, files)
	a.mu.Lock()
	a.state.NextCheck = time.Now().Add(60 * time.Millisecond)
	if e := a.persistState(); e != nil {
		t.Fatal(e)
	}
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.RunScheduler(ctx)
	deadline := time.After(4 * time.Second)
	for {
		if a.State().Active == v2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("scheduled check did not apply new commit")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if a.State().LastSuccess.IsZero() {
		t.Fatal("successful application not recorded")
	}
}
func TestConcurrentRequestRetainsOldSnapshot(t *testing.T) {
	m, source := newMock(t)
	files := communityFiles(t)
	m.publish(t, v1, files)
	a := mockApp(t, source)
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	started := make(chan struct{})
	finish := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
		fmt.Fprint(w, fixtureBody("empire-detail.html"))
	}))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	a.targetTransport = routeTransport{u, http.DefaultTransport}
	result := make(chan ScrapeResponse, 1)
	errch := make(chan error, 1)
	go func() {
		r, e := a.Scrape(context.Background(), ScrapeRequest{ScraperID: "AdultEmpire", Object: "scene", URL: "https://www.adultempire.com/10001/fixture-one.html"}, false)
		result <- r
		errch <- e
	}()
	<-started
	m.publish(t, v2, files)
	if e := a.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if a.State().Active != v2 {
		t.Fatal("update not activated")
	}
	close(finish)
	r := <-result
	if e := <-errch; e != nil {
		t.Fatal(e)
	}
	if r.RulesVersion != v1 || r.Results[0].RulesVersion != v1 {
		t.Fatal("in-flight request mixed versions", r)
	}
}
func TestArchiveTraversalAndLinksRejected(t *testing.T) {
	for _, header := range []tar.Header{{Name: "repo/../../escape", Mode: 0600, Size: 1}, {Name: "repo/file", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, {Name: "repo/C:/escape", Mode: 0600, Size: 1}} {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&header)
		if header.Size > 0 {
			_, _ = tw.Write([]byte("x"))
		}
		_ = tw.Close()
		_ = gz.Close()
		if e := extractArchive(&b, t.TempDir()); e == nil {
			t.Fatal("unsafe archive accepted", header.Name)
		}
	}
}
func TestSecretRedactionAndCrossOriginProtection(t *testing.T) {
	a := fixtureApp(t)
	c := a.Config()
	c.Scrapers["AdultEmpire"] = ScraperConfig{Enabled: true, Cookie: "secret-cookie", APIKey: "secret-key", Headers: map[string]string{"X-Token": "secret-header"}}
	if e := a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	h := a.Handler()
	r := httptest.NewRequest("GET", "http://localhost/api/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "secret-") {
		t.Fatal("secrets leaked")
	}
	var redacted Config
	_ = json.Unmarshal(w.Body.Bytes(), &redacted)
	restoreSecrets(&redacted, a.Config())
	if redacted.Scrapers["AdultEmpire"].APIKey != "secret-key" {
		t.Fatal("redacted config did not preserve secret")
	}
	r = httptest.NewRequest("POST", "http://localhost/api/reload", nil)
	r.Header.Set("Origin", "https://untrusted.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin operation allowed")
	}
}
func TestURLValidationAndPrivateNetworkBlock(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://user:pass@example.com/", "http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/", "http://10.0.0.1/", "https://example.com/#secret"} {
		if e := validateURL(raw, false); e == nil {
			t.Fatal("invalid target accepted", raw)
		}
	}
	rule, e := engine.LoadReader("test", strings.NewReader("name: test\nsceneByURL:\n - action: script\n   script: [python, test.py]\n   url: [example.com]\n"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = rule.Select("scene", "url", "https://evil.example/?example.com"); e == nil {
		t.Fatal("substring URL host spoof accepted")
	}
}
func TestCancellationAndOutputLimits(t *testing.T) {
	python := os.Getenv("TEST_PYTHON")
	if python == "" {
		t.Skip("set TEST_PYTHON to run process isolation checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, e := runProcess(ctx, python, []string{"-c", "import time; time.sleep(10)"}, t.TempDir(), os.Environ(), nil); e == nil {
		t.Fatal("timeout did not kill script")
	}
	if _, e := runProcess(context.Background(), python, []string{"-c", "print('x' * 3000000)"}, t.TempDir(), os.Environ(), nil); e == nil {
		t.Fatal("output limit not enforced")
	}
}
