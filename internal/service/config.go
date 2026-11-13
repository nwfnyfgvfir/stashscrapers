package service

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SourceConfig struct {
	Repository  string `json:"repository"`
	Branch      string `json:"branch"`
	APIBase     string `json:"api_base"`
	ArchiveBase string `json:"archive_base"`
}
type UpdateConfig struct {
	Enabled       bool      `json:"enabled"`
	IntervalHours float64   `json:"interval_hours"`
	Timezone      string    `json:"timezone"`
	DailyAt       string    `json:"daily_at"`
	Retain        int       `json:"retain"`
	Pin           string    `json:"pin"`
	PinUntil      time.Time `json:"pin_until"`
}
type ScraperConfig struct {
	Enabled        bool              `json:"enabled"`
	ScriptApproved bool              `json:"script_approved"`
	Headers        map[string]string `json:"headers,omitempty"`
	Cookie         string            `json:"cookie,omitempty"`
	APIKey         string            `json:"api_key,omitempty"`
	APIKeyHeader   string            `json:"api_key_header,omitempty"`
}
type Config struct {
	Listen              string                   `json:"listen"`
	Source              SourceConfig             `json:"source"`
	Update              UpdateConfig             `json:"update"`
	LocalDir            string                   `json:"local_dir"`
	Proxy               string                   `json:"proxy"`
	PythonBase          string                   `json:"python_base"`
	CDP                 string                   `json:"cdp"`
	TimeoutSeconds      int                      `json:"timeout_seconds"`
	Concurrency         int                      `json:"concurrency"`
	SiteIntervalMS      int                      `json:"site_interval_ms"`
	AllowPrivateTargets bool                     `json:"allow_private_targets"`
	AutoEnableNative    bool                     `json:"auto_enable_native"`
	Scrapers            map[string]ScraperConfig `json:"scrapers"`
}

func DefaultConfig() Config {
	return Config{
		Listen:         "127.0.0.1:8080",
		Source:         SourceConfig{Repository: "stashapp/CommunityScrapers", APIBase: "https://api.github.com", ArchiveBase: "https://codeload.github.com"},
		Update:         UpdateConfig{Enabled: true, IntervalHours: 24, Timezone: "Asia/Shanghai", Retain: 3},
		TimeoutSeconds: 30, Concurrency: 4, SiteIntervalMS: 1000, AutoEnableNative: true, Scrapers: map[string]ScraperConfig{},
	}
}
func (c Config) Validate() error {
	if c.CDP != "" {
		u, e := url.Parse(c.CDP)
		if e != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ws" && u.Scheme != "wss") {
			return errors.New("CDP must be an absolute HTTP(S) or WS(S) endpoint without credentials")
		}
	}
	if c.Update.Pin != "" && !shaPattern.MatchString(c.Update.Pin) {
		return errors.New("pin must be a full 40-character commit SHA")
	}
	if c.Concurrency < 1 || c.Concurrency > 64 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 300 || c.SiteIntervalMS < 0 || c.SiteIntervalMS > 60000 {
		return errors.New("invalid timeout, concurrency or site interval")
	}
	if c.Update.IntervalHours < 1 || c.Update.IntervalHours > 8760 || c.Update.Retain < 2 || c.Update.Retain > 20 {
		return errors.New("update interval must be 1..8760 hours; retain must be 2..20")
	}
	if _, err := time.LoadLocation(c.Update.Timezone); err != nil {
		return errors.New("unknown update timezone")
	}
	if c.Update.DailyAt != "" {
		if _, err := time.Parse("15:04", c.Update.DailyAt); err != nil {
			return errors.New("daily_at must be HH:MM")
		}
	}
	parts := strings.Split(c.Source.Repository, "/")
	if len(parts) != 2 || !safeRepo.MatchString(c.Source.Repository) {
		return errors.New("repository must be owner/name")
	}
	for _, base := range []string{c.Source.APIBase, c.Source.ArchiveBase} {
		if err := validateURL(base, true); err != nil {
			return errors.New("invalid upstream endpoint")
		}
	}
	if c.Proxy != "" {
		if err := validateURL(c.Proxy, true); err != nil {
			return errors.New("invalid proxy URL")
		}
	}
	if c.PythonBase != "" {
		if !filepath.IsAbs(c.PythonBase) {
			return errors.New("python_base must be an absolute executable path")
		}
		if st, e := os.Stat(c.PythonBase); e != nil || st.IsDir() {
			return errors.New("python_base does not exist")
		}
	}
	if c.LocalDir != "" {
		if !filepath.IsAbs(c.LocalDir) {
			return errors.New("local_dir must be absolute")
		}
		if st, e := os.Stat(c.LocalDir); e != nil || !st.IsDir() {
			return errors.New("local_dir does not exist")
		}
	}
	for _, s := range c.Scrapers {
		for k, v := range s.Headers {
			if strings.ContainsAny(k+v, "\r\n") {
				return errors.New("invalid header")
			}
		}
		if strings.ContainsAny(s.Cookie+s.APIKey+s.APIKeyHeader, "\r\n") {
			return errors.New("invalid cookie or API key")
		}
	}
	return nil
}
func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_ = f.Chmod(0600)
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// os.Rename replaces a regular file atomically on supported Windows/Unix filesystems.
	return os.Rename(tmp, path)
}
func loadConfig(path string) (Config, error) {
	c := DefaultConfig()
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, writeJSON(path, c)
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if c.Scrapers == nil {
		c.Scrapers = map[string]ScraperConfig{}
	}
	if e == nil {
		e = c.Validate()
	}
	return c, e
}
func nextCheck(now time.Time, c UpdateConfig) time.Time {
	if c.DailyAt == "" {
		return now.Add(time.Duration(c.IntervalHours * float64(time.Hour)))
	}
	loc, _ := time.LoadLocation(c.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	n := now.In(loc)
	clock, _ := time.Parse("15:04", c.DailyAt)
	t := time.Date(n.Year(), n.Month(), n.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}
