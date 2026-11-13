package service

import (
	"context"
	"errors"
	"io"
	"stashscrapers/internal/engine"
	"strings"
	"testing"
	"time"
)

func TestFragmentRewriteAndMissingParameters(t *testing.T) {
	b, _ := fixtures.ReadFile("testdata/community/1pondo.yml")
	r, e := engine.LoadReader("1pondo", strings.NewReader(string(b)))
	if e != nil {
		t.Fatal(e)
	}
	op, e := r.Select("scene", "fragment", "")
	if e != nil {
		t.Fatal(e)
	}
	fetch := func(_ context.Context, url string) (io.ReadCloser, error) {
		if url != "https://en.1pondo.tv/dyn/phpauto/movie_details/movie_id/010124_001.json" {
			t.Fatal("fragment replacement mismatch", url)
		}
		return io.NopCloser(strings.NewReader(fixtureBody("1pondo.json"))), nil
	}
	result, e := r.Run(context.Background(), "scene", op, map[string]interface{}{"filename": "1pondo-010124_001-example.mp4"}, engine.RunOptions{Fetch: fetch})
	if e != nil || len(result) != 1 {
		t.Fatal(result, e)
	}
	if _, e = r.Run(context.Background(), "scene", op, map[string]interface{}{"title": "insufficient"}, engine.RunOptions{Fetch: fetch}); !errors.Is(e, engine.ErrInput) {
		t.Fatal("missing fragment parameters not classified", e)
	}
}
func TestJavaScriptDeadline(t *testing.T) {
	raw := "name: Timeout test\nsceneByURL:\n - action: scrapeXPath\n   url: [example.com]\n   scraper: detail\nxPathScrapers:\n detail:\n  scene:\n   Title:\n    selector: //h1\n    postProcess:\n     - javascript: 'while (true) {}'\n"
	rule, e := engine.LoadReader("timeout", strings.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	op, _ := rule.Select("scene", "url", "https://example.com/")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, e = rule.Run(ctx, "scene", op, map[string]interface{}{"url": "https://example.com/"}, engine.RunOptions{Fetch: func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("<h1>fixture</h1>")), nil
	}})
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("JavaScript infinite loop was not cancelled", e)
	}
}
func TestDailyTimezoneAndUpdateLock(t *testing.T) {
	c := DefaultConfig().Update
	c.DailyAt = "03:30"
	at := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	next := nextCheck(at, c)
	if next.UTC() != time.Date(2026, 11, 14, 19, 30, 0, 0, time.UTC) {
		t.Fatal("daily timezone scheduling mismatch", next)
	}
	a, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.updateMu.Lock()
	e = a.Check(context.Background())
	a.updateMu.Unlock()
	var api *APIError
	if !errors.As(e, &api) || api.Code != "busy" {
		t.Fatal("update lock did not reject concurrent mutation", e)
	}
}
