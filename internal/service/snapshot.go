package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v2"
	"stashscrapers/internal/engine"
)

type Entry struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Operations   []Capability `json:"operations"`
	Enabled      bool         `json:"enabled"`
	State        string       `json:"state"`
	Reason       string       `json:"reason,omitempty"`
	Verification string       `json:"verification"`
	rule         *engine.Rule
	path         string
}
type Capability struct {
	engine.Operation
	State        string   `json:"state"`
	Reason       string   `json:"reason,omitempty"`
	Dependencies []string `json:"dependencies"`
}
type Manifest struct {
	Version       string    `json:"version"`
	Engine        string    `json:"engine"`
	Source        string    `json:"source"`
	Python        string    `json:"python,omitempty"`
	PythonVersion string    `json:"python_version,omitempty"`
	Packages      []string  `json:"packages"`
	Created       time.Time `json:"created"`
}
type Snapshot struct {
	Version, Root string
	Entries       map[string]*Entry
	Manifest      Manifest
	refs          int
}
type Event struct {
	Version      string    `json:"version"`
	Action       string    `json:"action"`
	Result       string    `json:"result"`
	Reason       string    `json:"reason,omitempty"`
	At           time.Time `json:"at"`
	Engine       string    `json:"engine"`
	Dependencies *Manifest `json:"dependencies,omitempty"`
}
type UpdateState struct {
	Active         string            `json:"active"`
	Engine         string            `json:"engine"`
	Upstream       string            `json:"upstream"`
	Branch         string            `json:"branch"`
	LastCheck      time.Time         `json:"last_check"`
	NextCheck      time.Time         `json:"next_check"`
	LastSuccess    time.Time         `json:"last_success"`
	Phase          string            `json:"phase"`
	Failure        string            `json:"failure"`
	ETag           string            `json:"etag"`
	SourceKey      string            `json:"source_key"`
	RateLimitUntil time.Time         `json:"rate_limit_until"`
	Failed         map[string]string `json:"failed_commits"`
	History        []Event           `json:"history"`
}
type LogEntry struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}
type App struct {
	Data       string
	mu         sync.RWMutex
	config     Config
	state      UpdateState
	logs       []LogEntry
	updateMu   sync.Mutex
	snapMu     sync.Mutex
	current    atomic.Pointer[Snapshot]
	snapshots  map[string]*Snapshot
	references map[string]int
	limiter    limiter
	slots      chan struct{}
	wake       chan struct{}
	// Injectable clocks and HTTP clients keep scheduler and archive tests deterministic.
	now             func() time.Time
	upstreamClient  *http.Client
	targetTransport http.RoundTripper
}

func New(data string) (*App, error) {
	root, err := filepath.Abs(data)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	c, err := loadConfig(filepath.Join(root, "config.json"))
	if err != nil {
		return nil, err
	}
	a := &App{Data: root, config: c, slots: make(chan struct{}, 64), wake: make(chan struct{}, 1), snapshots: map[string]*Snapshot{}, references: map[string]int{}, now: time.Now, upstreamClient: &http.Client{Timeout: 3 * time.Minute}}
	a.state = UpdateState{Engine: engine.Version, Phase: "idle", Failed: map[string]string{}, History: []Event{}}
	b, e := os.ReadFile(filepath.Join(root, "state.json"))
	if e == nil {
		if e = json.Unmarshal(b, &a.state); e != nil {
			return nil, fmt.Errorf("invalid state.json: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if a.state.Failed == nil {
		a.state.Failed = map[string]string{}
	}
	// A newer engine may support a commit rejected by the previous engine.
	if a.state.Engine != "" && a.state.Engine != engine.Version {
		a.state.Failed = map[string]string{}
	}
	a.state.Engine = engine.Version
	a.state.Phase = "idle"
	if a.state.Active != "" {
		candidates := []string{a.state.Active}
		for i := len(a.state.History) - 1; i >= 0; i-- {
			h := a.state.History[i]
			if h.Result == "ok" {
				candidates = append(candidates, h.Version)
			}
		}
		for _, v := range candidates {
			if !shaPattern.MatchString(v) {
				continue
			}
			s, e := a.loadSnapshot(filepath.Join(root, "versions", v), v)
			if e == nil {
				e = runRegression(context.Background(), s)
			}
			if e == nil {
				a.current.Store(s)
				a.snapshots[v] = s
				if a.state.Active != v {
					a.state.Failure = "startup recovered a previous usable snapshot"
					a.state.Active = v
				}
				break
			}
		}
		if a.current.Load() == nil {
			a.state.Active = ""
			a.state.Failure = "installed snapshots could not be loaded"
		}
	}
	if a.state.NextCheck.IsZero() {
		a.state.NextCheck = a.now()
	}
	if err = a.persistState(); err != nil {
		return nil, err
	}
	if c.LocalDir != "" && a.current.Load() == nil {
		if err = a.InstallLocal(context.Background()); err != nil {
			return nil, err
		}
	}
	return a, nil
}
func (a *App) Config() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	b, _ := json.Marshal(a.config)
	var c Config
	_ = json.Unmarshal(b, &c)
	return c
}
func (a *App) State() UpdateState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	b, _ := json.Marshal(a.state)
	var s UpdateState
	_ = json.Unmarshal(b, &s)
	return s
}
func (a *App) persistState() error { return writeJSON(filepath.Join(a.Data, "state.json"), a.state) }
func (a *App) SaveConfig(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config
	if err := writeJSON(filepath.Join(a.Data, "config.json"), c); err != nil {
		return err
	}
	a.config = c
	if old.Update.DailyAt != c.Update.DailyAt || old.Update.IntervalHours != c.Update.IntervalHours || old.Update.Timezone != c.Update.Timezone {
		a.state.NextCheck = nextCheck(a.now(), c.Update)
		if err := a.persistState(); err != nil {
			return err
		}
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return nil
}
func (a *App) phase(phase string) {
	a.mu.Lock()
	a.state.Phase = phase
	_ = a.persistState()
	a.mu.Unlock()
}
func (a *App) log(level, message string) {
	// Only fixed diagnostic messages are logged. Never include URLs, input, headers or script stderr.
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs = append(a.logs, LogEntry{a.now(), level, message})
	if len(a.logs) > 200 {
		a.logs = a.logs[len(a.logs)-200:]
	}
}
func (a *App) acquire() (*Snapshot, func()) {
	a.snapMu.Lock()
	s := a.current.Load()
	if s != nil {
		a.references[s.Version]++
	}
	a.snapMu.Unlock()
	return s, func() {
		if s != nil {
			a.snapMu.Lock()
			a.references[s.Version]--
			a.snapMu.Unlock()
		}
	}
}
func (a *App) loadSnapshot(dir, version string) (*Snapshot, error) {
	var manifest Manifest
	b, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &manifest); e != nil {
		return nil, e
	}
	if manifest.Version != version {
		return nil, errors.New("snapshot manifest mismatch")
	}
	return loadRegistry(filepath.Join(dir, "repo"), version, manifest)
}
func loadRegistry(root, version string, manifest Manifest) (*Snapshot, error) {
	s := &Snapshot{Version: version, Root: root, Entries: map[string]*Entry{}, Manifest: manifest}
	scan := root
	if info, e := os.Stat(filepath.Join(root, "scrapers")); e == nil && info.IsDir() {
		scan = filepath.Join(root, "scrapers")
	}
	err := filepath.WalkDir(scan, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed in a rule snapshot")
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := strings.ToLower(filepath.Ext(path)); ext != ".yml" && ext != ".yaml" {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var head struct {
			Name string `yaml:"name"`
		}
		if e = yaml.Unmarshal(b, &head); e != nil {
			return fmt.Errorf("invalid YAML %s", filepath.Base(path))
		}
		if head.Name == "" {
			return nil
		}
		rel, _ := filepath.Rel(scan, path)
		id := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
		entry := &Entry{ID: id, Name: head.Name, Version: version, Operations: []Capability{}, State: "unsupported", Verification: "unverified", path: path}
		entry.rule, e = engine.Load(path)
		if e != nil {
			entry.Reason = "engine cannot load this definition: " + e.Error()
		} else if len(entry.rule.Operations()) == 0 {
			entry.Reason = "no scene or performer actions"
		}
		s.Entries[id] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(s.Entries) == 0 {
		return nil, errors.New("snapshot contains no scraper definitions")
	}
	return s, nil
}
func (a *App) describe(e *Entry, s *Snapshot, c Config) Entry {
	ret := *e
	settings, explicit := c.Scrapers[e.ID]
	ret.Enabled = settings.Enabled
	if !explicit {
		ret.Enabled = c.AutoEnableNative
	}
	if e.rule == nil || len(e.rule.Operations()) == 0 {
		return ret
	}
	ret.State = "unsupported"
	ret.Reason = "no supported actions"
	ret.Operations = []Capability{}
	for _, op := range e.rule.Operations() {
		cap := Capability{Operation: op, State: "available", Dependencies: []string{"Go " + engine.Version}}
		if op.Action == "stash" || e.rule.HasStashServer() {
			cap.State = "unsupported"
			cap.Reason = "requires the full Stash API"
		}
		if op.Action == "script" {
			cap.Dependencies = append(cap.Dependencies, "Python >= 3.12", "isolated snapshot environment")
			// Only this audited community script has an exercised dependency contract.
			if e.ID != "RealjamVR/RealjamVR" {
				cap.State = "unsupported"
				cap.Reason = "script and transitive dependencies have not been audited for standalone execution"
			} else if !settings.ScriptApproved {
				cap.State = "needs_configuration"
				cap.Reason = "approve this trusted script in scraper settings"
			} else if s.Manifest.Python == "" {
				cap.State = "missing_dependency"
				cap.Reason = "configure python_base, then install or reload this version"
			} else if _, err := os.Stat(s.Manifest.Python); err != nil {
				cap.State = "missing_dependency"
				cap.Reason = "snapshot Python executable is missing"
			}
		}
		if e.ID == "Filename/Filename" {
			cap.State = "unsupported"
			cap.Reason = "requires Stash GraphQL findScene and media-library file paths"
			cap.Dependencies = append(cap.Dependencies, "full Stash API")
		}
		if (op.Action == "script" || e.rule.Driver() != nil && e.rule.Driver().UseCDP) && cap.State == "available" && !c.AllowPrivateTargets {
			cap.State = "needs_configuration"
			cap.Reason = "external Python/browser networking requires allow_private_targets acknowledgement"
		}
		if d := e.rule.Driver(); d != nil {
			if d.UseSurf {
				cap.State = "unsupported"
				cap.Reason = "upstream useSurf TLS impersonation driver is not extracted"
			}
			if d.UseCDP {
				cap.Dependencies = append(cap.Dependencies, "remote CDP browser")
				if c.CDP == "" {
					cap.State = "missing_dependency"
					cap.Reason = "CDP browser endpoint is not configured"
				}
				if op.Action == "scrapeJson" {
					cap.State = "unsupported"
					cap.Reason = "JSON with CDP is not implemented by this browser adapter"
				}
			}
			if !d.UseCDP && (len(d.Clicks) > 0 || d.Sleep > 0) {
				cap.State = "unsupported"
				cap.Reason = "browser click/sleep driver requires useCDP"
			}
		}
		if op.Object == "performer" && (op.Operation == "fragment" || op.Operation == "query_fragment") && op.Action != "script" {
			cap.State = "unsupported"
			cap.Reason = "upstream mapped performer fragment action is unsupported"
		}
		ret.Operations = append(ret.Operations, cap)
		priority := map[string]int{"unsupported": 0, "missing_dependency": 1, "needs_configuration": 2, "available": 3}
		if priority[cap.State] > priority[ret.State] {
			ret.State = cap.State
			ret.Reason = cap.Reason
		}
	}
	if ret.State == "available" {
		ret.Reason = "runtime prerequisites satisfied; target website is not yet verified"
	}
	if fixtureVerified(e.ID) {
		ret.Verification = "offline_fixture"
	}
	return ret
}
func (a *App) List() []Entry {
	s, done := a.acquire()
	defer done()
	ret := []Entry{}
	if s == nil {
		return ret
	}
	c := a.Config()
	for _, e := range s.Entries {
		ret = append(ret, a.describe(e, s, c))
	}
	for id, settings := range c.Scrapers {
		if _, ok := s.Entries[id]; !ok {
			ret = append(ret, Entry{ID: id, Name: id, Version: s.Version, Enabled: settings.Enabled, State: "unsupported", Reason: "removed or renamed by upstream; explicit settings retained", Operations: []Capability{}, Verification: "unverified"})
		}
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].ID < ret[j].ID })
	return ret
}
func copyTree(src, dst string) error {
	abs, e := filepath.Abs(src)
	if e != nil {
		return e
	}
	dstAbs, _ := filepath.Abs(dst)
	if dstAbs == abs || strings.HasPrefix(dstAbs, abs+string(os.PathSeparator)) {
		return errors.New("snapshot destination cannot be inside source")
	}
	var total int64
	return filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(abs, path)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("local symlinks are forbidden")
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		total += info.Size()
		if total > 256<<20 {
			return errors.New("local snapshot exceeds 256 MiB")
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(filepath.Join(dst, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		closeErr := out.Close()
		if e != nil {
			return e
		}
		return closeErr
	})
}
func hashTree(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		_, _ = io.WriteString(h, filepath.ToSlash(rel)+"\x00")
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		_, e = io.Copy(h, f)
		f.Close()
		return e
	})
	return hex.EncodeToString(h.Sum(nil))[:40], err
}
