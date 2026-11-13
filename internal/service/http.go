package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"stashscrapers/internal/engine"
	"stashscrapers/internal/web"
)

func jsonResponse(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (a *App) sendError(w http.ResponseWriter, err error) {
	var api *APIError
	if !errors.As(err, &api) {
		api = &APIError{"internal_error", "operation failed; inspect update status", 500}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		api = &APIError{"timeout", "operation timed out", 504}
	} else if errors.Is(err, context.Canceled) {
		api = &APIError{"cancelled", "operation was cancelled", 499}
	}
	api.Message = a.redact(api.Message)
	jsonResponse(w, api.Status, map[string]interface{}{"error": api})
}
func decode(w http.ResponseWriter, r *http.Request, out interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return failure("input_error", "request must be a valid JSON object with known fields", 400)
	}
	var extra interface{}
	if d.Decode(&extra) != io.EOF {
		return failure("input_error", "request must contain one JSON object", 400)
	}
	return nil
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		s := a.State()
		jsonResponse(w, 200, map[string]interface{}{"status": "ok", "ready": s.Active != "", "engine": engine.Version, "rules_version": s.Active, "update_phase": s.Phase})
	})
	mux.HandleFunc("GET /api/scrapers", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]interface{}{"scrapers": a.List()})
	})
	mux.HandleFunc("GET /api/scrapers/{id...}", func(w http.ResponseWriter, r *http.Request) {
		for _, e := range a.List() {
			if e.ID == r.PathValue("id") {
				jsonResponse(w, 200, e)
				return
			}
		}
		a.sendError(w, failure("not_found", "unknown scraper", 404))
	})
	mux.HandleFunc("PATCH /api/scrapers/{id...}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled        *bool             `json:"enabled"`
			ScriptApproved *bool             `json:"script_approved"`
			Cookie         *string           `json:"cookie"`
			Headers        map[string]string `json:"headers"`
			APIKey         *string           `json:"api_key"`
			APIKeyHeader   *string           `json:"api_key_header"`
		}
		if e := decode(w, r, &body); e != nil {
			a.sendError(w, e)
			return
		}
		id := r.PathValue("id")
		exists := false
		for _, e := range a.List() {
			if e.ID == id {
				exists = true
			}
		}
		if !exists {
			a.sendError(w, failure("not_found", "unknown scraper", 404))
			return
		}
		c := a.Config()
		s, explicit := c.Scrapers[id]
		if !explicit {
			s.Enabled = c.AutoEnableNative
		}
		if body.Enabled != nil {
			s.Enabled = *body.Enabled
		}
		if body.ScriptApproved != nil {
			s.ScriptApproved = *body.ScriptApproved
		}
		if body.Cookie != nil {
			s.Cookie = *body.Cookie
		}
		if body.Headers != nil {
			s.Headers = body.Headers
		}
		if body.APIKey != nil {
			s.APIKey = *body.APIKey
		}
		if body.APIKeyHeader != nil {
			s.APIKeyHeader = *body.APIKeyHeader
		}
		c.Scrapers[id] = s
		if e := a.SaveConfig(c); e != nil {
			a.sendError(w, failure("input_error", e.Error(), 400))
			return
		}
		jsonResponse(w, 200, map[string]bool{"saved": true})
	})
	scrape := func(search bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var req ScrapeRequest
			if e := decode(w, r, &req); e != nil {
				a.sendError(w, e)
				return
			}
			out, e := a.Scrape(r.Context(), req, search)
			if e != nil {
				a.sendError(w, e)
				return
			}
			jsonResponse(w, 200, out)
		}
	}
	mux.HandleFunc("POST /api/search", scrape(true))
	mux.HandleFunc("POST /api/detail", scrape(false))
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, redactedConfig(a.Config())) })
	mux.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		c := DefaultConfig()
		if e := decode(w, r, &c); e != nil {
			a.sendError(w, e)
			return
		}
		restoreSecrets(&c, a.Config())
		if e := a.SaveConfig(c); e != nil {
			a.sendError(w, failure("input_error", e.Error(), 400))
			return
		}
		jsonResponse(w, 200, redactedConfig(c))
	})
	mux.HandleFunc("POST /api/reload", func(w http.ResponseWriter, r *http.Request) {
		if e := a.Reload(r.Context()); e != nil {
			a.sendError(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"reloaded": true})
	})
	mux.HandleFunc("GET /api/dependencies", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, a.DependencyStatus()) })
	mux.HandleFunc("GET /api/updates", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]interface{}{"state": a.State(), "schedule": a.Config().Update, "retained": a.retainedVersions()})
	})
	mux.HandleFunc("POST /api/updates/check", func(w http.ResponseWriter, r *http.Request) {
		if e := a.Check(r.Context()); e != nil {
			a.sendError(w, e)
			return
		}
		jsonResponse(w, 200, a.State())
	})
	mux.HandleFunc("POST /api/install", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Source string `json:"source"`
		}
		if e := decode(w, r, &body); e != nil {
			a.sendError(w, e)
			return
		}
		var e error
		if body.Source == "local" {
			e = a.InstallLocal(r.Context())
		} else if body.Source == "upstream" {
			e = a.Check(r.Context())
		} else {
			e = failure("input_error", "source must be local or upstream", 400)
		}
		if e != nil {
			a.sendError(w, e)
			return
		}
		jsonResponse(w, 200, a.State())
	})
	mux.HandleFunc("POST /api/updates/rollback", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Version string `json:"version"`
		}
		if e := decode(w, r, &body); e != nil {
			a.sendError(w, e)
			return
		}
		if e := a.Rollback(r.Context(), body.Version); e != nil {
			a.sendError(w, e)
			return
		}
		jsonResponse(w, 200, a.State())
	})
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		logs := append([]LogEntry{}, a.logs...)
		a.mu.RUnlock()
		jsonResponse(w, 200, map[string]interface{}{"logs": logs})
	})
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(web.OpenAPI)
	})
	mux.HandleFunc("GET /api/source", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="stashscrapers-source.tar.gz"`)
		_, _ = w.Write(web.Source)
	})
	mux.Handle("GET /", web.Handler())
	token := os.Getenv("STASHSCRAPERS_TOKEN")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/health" && r.URL.Path != "/api/openapi.json" && r.URL.Path != "/api/source" {
			if token != "" && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(token)) != 1 {
				a.sendError(w, failure("unauthorized", "provide the configured service Bearer token", 401))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || !strings.EqualFold(u.Host, r.Host) {
					a.sendError(w, failure("input_error", "cross-origin administration is forbidden", 403))
					return
				}
			}
			if token == "" {
				host, _, _ := net.SplitHostPort(a.Config().Listen)
				if host == "127.0.0.1" || host == "localhost" {
					requestHost := r.Host
					if h, _, e := net.SplitHostPort(requestHost); e == nil {
						requestHost = h
					}
					if requestHost != "127.0.0.1" && requestHost != "localhost" && requestHost != "::1" {
						a.sendError(w, failure("input_error", "untrusted Host header", 403))
						return
					}
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func redactedConfig(c Config) Config {
	for id, s := range c.Scrapers {
		if s.Cookie != "" {
			s.Cookie = "__REDACTED__"
		}
		if s.APIKey != "" {
			s.APIKey = "__REDACTED__"
		}
		for k := range s.Headers {
			s.Headers[k] = "__REDACTED__"
		}
		c.Scrapers[id] = s
	}
	return c
}
func restoreSecrets(c *Config, old Config) {
	for id, s := range c.Scrapers {
		prev := old.Scrapers[id]
		if s.Cookie == "__REDACTED__" {
			s.Cookie = prev.Cookie
		}
		if s.APIKey == "__REDACTED__" {
			s.APIKey = prev.APIKey
		}
		for k, v := range s.Headers {
			if v == "__REDACTED__" {
				s.Headers[k] = prev.Headers[k]
			}
		}
		c.Scrapers[id] = s
	}
}
func (a *App) retainedVersions() []Manifest {
	out := []Manifest{}
	entries, _ := os.ReadDir(filepath.Join(a.Data, "versions"))
	for _, d := range entries {
		if !shaPattern.MatchString(d.Name()) {
			continue
		}
		b, e := os.ReadFile(filepath.Join(a.Data, "versions", d.Name(), "manifest.json"))
		if e != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(b, &m) == nil {
			out = append(out, m)
		}
	}
	return out
}
