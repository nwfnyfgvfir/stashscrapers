package service

import (
	"context"
	"errors"
	"fmt"
	"stashscrapers/internal/engine"
	"strings"
	"time"
)

type ScrapeRequest struct {
	ScraperID    string                 `json:"scraper_id"`
	Object       string                 `json:"object"`
	Query        string                 `json:"query"`
	URL          string                 `json:"url"`
	Fragment     map[string]interface{} `json:"fragment"`
	Operation    string                 `json:"operation"`
	RulesVersion string                 `json:"rules_version"`
}
type Result struct {
	Title        *string                `json:"title"`
	Name         *string                `json:"name"`
	Code         *string                `json:"code"`
	Details      *string                `json:"details"`
	Date         *string                `json:"date"`
	Duration     interface{}            `json:"duration"`
	Studio       interface{}            `json:"studio"`
	Director     *string                `json:"director"`
	Performers   interface{}            `json:"performers"`
	Tags         interface{}            `json:"tags"`
	Images       []string               `json:"images"`
	URLs         []string               `json:"urls"`
	ScraperID    string                 `json:"scraper_id"`
	RulesVersion string                 `json:"rules_version"`
	Raw          map[string]interface{} `json:"raw"`
	Warnings     []string               `json:"warnings"`
}
type ScrapeResponse struct {
	Results      []Result `json:"results"`
	RulesVersion string   `json:"rules_version"`
	Warnings     []string `json:"warnings"`
}

func textPtr(raw map[string]interface{}, key string) *string {
	if s, ok := raw[key].(string); ok && s != "" {
		return &s
	}
	return nil
}
func stringValues(v interface{}) []string {
	out := []string{}
	switch t := v.(type) {
	case string:
		if t != "" {
			out = append(out, t)
		}
	case []interface{}:
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, t...)
	}
	return out
}
func normalize(raw map[string]interface{}, id, version string) Result {
	images := append(stringValues(raw["images"]), stringValues(raw["image"])...)
	urls := append(stringValues(raw["urls"]), stringValues(raw["url"])...)
	unique := func(in []string) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, s := range in {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
		return out
	}
	return Result{Title: textPtr(raw, "title"), Name: textPtr(raw, "name"), Code: textPtr(raw, "code"), Details: textPtr(raw, "details"), Date: textPtr(raw, "date"), Duration: raw["duration"], Studio: raw["studio"], Director: textPtr(raw, "director"), Performers: raw["performers"], Tags: raw["tags"], Images: unique(images), URLs: unique(urls), ScraperID: id, RulesVersion: version, Raw: raw, Warnings: []string{"runtime result; field completeness and target accuracy are not guaranteed"}}
}
func (a *App) Scrape(ctx context.Context, req ScrapeRequest, search bool) (ScrapeResponse, error) {
	empty := ScrapeResponse{Results: []Result{}, Warnings: []string{}}
	if req.ScraperID == "" || (req.Object != "scene" && req.Object != "performer") {
		return empty, failure("input_error", "scraper_id and object (scene/performer) are required", 400)
	}
	c := a.Config()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	// A short critical section reserves one of the configured slots; cancellation is observed while queued.
	for {
		a.mu.Lock()
		if len(a.slots) < c.Concurrency {
			a.slots <- struct{}{}
			a.mu.Unlock()
			break
		}
		a.mu.Unlock()
		t := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
			return empty, ctx.Err()
		case <-t.C:
		}
	}
	defer func() { a.mu.Lock(); <-a.slots; a.mu.Unlock() }()
	s, done := a.acquire()
	defer done()
	if s == nil {
		return empty, failure("missing_dependency", "rules are not installed; inspect update status", 503)
	}
	empty.RulesVersion = s.Version
	if req.RulesVersion != "" && req.RulesVersion != s.Version {
		return empty, failure("version_conflict", "rules changed after the search; repeat the search", 409)
	}
	entry, ok := s.Entries[req.ScraperID]
	if !ok {
		return empty, failure("not_found", "scraper is absent in the active snapshot", 404)
	}
	description := a.describe(entry, s, c)
	if !description.Enabled {
		return empty, failure("disabled", "scraper is disabled", 409)
	}
	if entry.rule == nil {
		return empty, failure("unsupported", description.Reason, 422)
	}
	input := map[string]interface{}{}
	op := req.Operation
	if search {
		if strings.TrimSpace(req.Query) == "" {
			return empty, failure("input_error", "query is required for name search; URL-only rules cannot search", 400)
		}
		op = "name"
		input["name"] = req.Query
	} else if req.URL != "" {
		op = "url"
		input["url"] = req.URL
	} else if len(req.Fragment) > 0 {
		for k, v := range req.Fragment {
			input[k] = v
		}
		if op == "" {
			op = "fragment"
		}
		if op != "fragment" && op != "query_fragment" {
			return empty, failure("input_error", "fragment operation must be fragment or query_fragment", 400)
		}
	} else {
		return empty, failure("input_error", "URL or fragment is required for details", 400)
	}
	if u, ok := input["url"].(string); ok {
		if e := validateURL(u, c.AllowPrivateTargets); e != nil {
			return empty, failure("input_error", e.Error(), 400)
		}
	}
	action, err := entry.rule.Select(req.Object, op, req.URL)
	if err != nil {
		return empty, failure("unsupported", err.Error(), 422)
	}
	for _, cap := range description.Operations {
		if cap.Object == req.Object && cap.Operation.Operation == op && cap.Action == action.Action && cap.State != "available" {
			return empty, failure(cap.State, cap.Reason, 424)
		}
	}
	raw, err := entry.rule.Run(ctx, req.Object, action, input, engineRunOptions(a, s, entry, c))
	if ctx.Err() != nil {
		a.log("warn", "scrape cancelled or timed out; rules "+s.Version[:12])
		return empty, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, engine.ErrInput) {
			return empty, failure("input_error", err.Error(), 400)
		}
		var known *APIError
		if errors.As(err, &known) {
			a.log("error", "scrape failed ["+known.Code+"]; rules "+s.Version[:12])
			return empty, err
		}
		a.log("error", "scrape failed during parsing or post-processing")
		return empty, failure("parse_error", fmt.Sprintf("rule execution failed: %s", a.redact(err.Error())), 502)
	}
	for _, one := range raw {
		if len(one) > 0 {
			empty.Results = append(empty.Results, normalize(one, entry.ID, s.Version))
		}
	}
	if len(empty.Results) == 0 {
		return empty, failure("not_found", "no result; the target may have changed its response format", 404)
	}
	if search {
		empty.Warnings = append(empty.Warnings, "choose a candidate explicitly before requesting details")
	}
	a.log("info", "scrape completed with pinned rules "+s.Version[:12])
	return empty, nil
}
func (a *App) redact(s string) string {
	c := a.Config()
	for _, setting := range c.Scrapers {
		for _, secret := range append([]string{setting.Cookie, setting.APIKey}, headerValues(setting.Headers)...) {
			if secret != "" {
				s = strings.ReplaceAll(s, secret, "[redacted]")
			}
		}
	}
	if c.Proxy != "" {
		s = strings.ReplaceAll(s, c.Proxy, "[redacted proxy]")
	}
	return s
}
func headerValues(m map[string]string) []string {
	out := []string{}
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
