package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"stashscrapers/internal/engine"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var safeRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func (a *App) RunScheduler(ctx context.Context) {
	for {
		c := a.Config()
		s := a.State()
		delay := time.Until(s.NextCheck)
		if !c.Update.Enabled {
			delay = 24 * time.Hour
		}
		if delay < 0 {
			delay = 0
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-a.wake:
			t.Stop()
			continue
		case <-t.C:
		}
		if !a.Config().Update.Enabled {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := a.Check(checkCtx)
		cancel()
		var busy *APIError
		if errors.As(err, &busy) && busy.Code == "busy" {
			select {
			case <-ctx.Done():
				return
			case <-a.wake:
			case <-time.After(time.Second):
			}
		}
	}
}
func (a *App) Check(ctx context.Context) (err error) {
	if !a.updateMu.TryLock() {
		return failure("busy", "another update, reload or rollback is running", 409)
	}
	defer a.updateMu.Unlock()
	c := a.Config()
	now := a.now()
	a.mu.Lock()
	a.state.LastCheck = now
	a.state.NextCheck = nextCheck(now, c.Update)
	a.state.Phase = "checking"
	a.state.Failure = ""
	err = a.persistState()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			a.mu.Lock()
			a.state.Phase = "failed"
			a.state.Failure = err.Error()
			if len(a.state.History) == 0 || a.state.History[len(a.state.History)-1].Result != "failed" || a.state.History[len(a.state.History)-1].At.Before(now) {
				a.state.History = append(a.state.History, Event{Version: a.state.Upstream, Action: "update", Result: "failed", Reason: err.Error(), At: a.now(), Engine: engine.Version})
			}
			if len(a.state.History) > 100 {
				a.state.History = a.state.History[len(a.state.History)-100:]
			}
			_ = a.persistState()
			a.mu.Unlock()
			a.log("error", "upstream update failed; current snapshot retained")
		}
	}()
	state := a.State()
	if state.RateLimitUntil.After(now) {
		return errors.New("upstream rate limit is active until " + state.RateLimitUntil.Format(time.RFC3339))
	}
	branch := c.Source.Branch
	if branch == "" {
		var meta struct {
			DefaultBranch string `json:"default_branch"`
		}
		if _, err = a.getJSON(ctx, c.Source.APIBase+"/repos/"+c.Source.Repository, "", &meta); err != nil {
			return err
		}
		branch = meta.DefaultBranch
		if branch == "" {
			return errors.New("upstream did not provide its default branch")
		}
	}
	sourceKey := c.Source.APIBase + "/" + c.Source.Repository + "/" + branch
	etag := ""
	if state.SourceKey == sourceKey {
		etag = state.ETag
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	nextETag, e := a.getJSON(ctx, c.Source.APIBase+"/repos/"+c.Source.Repository+"/commits/"+urlPathEscape(branch), etag, &commit)
	if e != nil {
		return e
	}
	if commit.SHA == "" {
		commit.SHA = state.Upstream
	}
	if !shaPattern.MatchString(commit.SHA) {
		return errors.New("upstream returned an invalid commit SHA")
	}
	a.mu.Lock()
	a.state.Upstream = commit.SHA
	a.state.Branch = branch
	a.state.SourceKey = sourceKey
	if nextETag != "" {
		a.state.ETag = nextETag
	}
	_ = a.persistState()
	a.mu.Unlock()
	desired := commit.SHA
	if c.Update.Pin != "" && (c.Update.PinUntil.IsZero() || now.Before(c.Update.PinUntil)) {
		desired = c.Update.Pin
	}
	if !shaPattern.MatchString(desired) {
		return errors.New("pin must be a full commit SHA")
	}
	if desired == a.State().Active {
		a.phase("idle")
		return nil
	}
	if why, failed := state.Failed[desired]; failed {
		a.phase("blocked")
		a.mu.Lock()
		a.state.Failure = "commit previously failed validation: " + why
		_ = a.persistState()
		a.mu.Unlock()
		return nil
	}
	if s, e := a.loadSnapshot(filepath.Join(a.Data, "versions", desired), desired); e == nil {
		if e = a.validateTransition(ctx, s); e != nil {
			return e
		}
		return a.activate(s, "apply")
	}
	a.phase("downloading")
	stage, e := os.MkdirTemp(a.stageRoot(), "candidate-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	repo := filepath.Join(stage, "repo")
	if e = os.MkdirAll(repo, 0700); e != nil {
		return e
	}
	archiveURL := strings.TrimSuffix(c.Source.ArchiveBase, "/") + "/" + c.Source.Repository + "/tar.gz/" + desired
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "stashscrapers/0.1")
	resp, e := a.upstreamClient.Do(req)
	if e != nil {
		return errors.New("upstream archive download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("archive returned HTTP %d", resp.StatusCode)
	}
	if e = extractArchive(resp.Body, repo); e != nil {
		return fmt.Errorf("incomplete or unsafe archive: %w", e)
	}
	return a.prepareAndActivate(ctx, stage, desired, c.Source.Repository+"@"+desired)
}
func urlPathEscape(v string) string {
	return url.PathEscape(v)
}
func (a *App) stageRoot() string {
	p := filepath.Join(a.Data, "staging")
	_ = os.MkdirAll(p, 0700)
	return p
}
func (a *App) getJSON(ctx context.Context, u, etag string, out interface{}) (string, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if e != nil {
		return "", e
	}
	req.Header.Set("User-Agent", "stashscrapers/0.1")
	req.Header.Set("Accept", "application/vnd.github+json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, e := a.upstreamClient.Do(req)
	if e != nil {
		return "", errors.New("upstream API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 304 {
		return etag, nil
	}
	if resp.StatusCode == 429 || resp.StatusCode == 403 {
		reset := a.now().Add(time.Hour)
		if raw := resp.Header.Get("X-RateLimit-Reset"); raw != "" {
			var unix int64
			if _, e = fmt.Sscan(raw, &unix); e == nil {
				reset = time.Unix(unix, 0)
			}
		}
		a.mu.Lock()
		a.state.RateLimitUntil = reset
		if a.state.NextCheck.Before(reset) {
			a.state.NextCheck = reset
		}
		_ = a.persistState()
		a.mu.Unlock()
		return "", errors.New("upstream API denied or rate limited the request")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("upstream API returned HTTP %d", resp.StatusCode)
	}
	e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
	if e != nil {
		return "", errors.New("invalid upstream API JSON")
	}
	return resp.Header.Get("ETag"), nil
}
func extractArchive(body io.Reader, dest string) error {
	gz, e := gzip.NewReader(io.LimitReader(body, 64<<20))
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	count := 0
	archiveRoot := ""
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		count++
		if count > 50000 {
			return errors.New("too many archive entries")
		}
		// GitHub's git archives contain a global PAX commit metadata header.
		// It is not a filesystem entry and must not become the archive root.
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := h.Name
		if strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
			return errors.New("unsafe archive path")
		}
		clean := path.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("archive traversal rejected")
		}
		parts := strings.SplitN(clean, "/", 2)
		if archiveRoot == "" {
			archiveRoot = parts[0]
		}
		if parts[0] != archiveRoot {
			return errors.New("archive must have a single root")
		}
		if len(parts) < 2 {
			continue
		}
		rel := parts[1]
		if rel == "." {
			continue
		}
		if strings.HasPrefix(rel, "../") {
			return errors.New("unsafe relative path")
		}
		full := filepath.Join(dest, filepath.FromSlash(rel))
		check, e := filepath.Rel(dest, full)
		if e != nil || check == ".." || strings.HasPrefix(check, ".."+string(os.PathSeparator)) {
			return errors.New("archive escapes staging")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if e = os.MkdirAll(full, 0700); e != nil {
				return e
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || h.Size > 32<<20 {
				return errors.New("archive file exceeds limit")
			}
			total += h.Size
			if total > 256<<20 {
				return errors.New("uncompressed archive exceeds limit")
			}
			if e = os.MkdirAll(filepath.Dir(full), 0700); e != nil {
				return e
			}
			f, e := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, copyErr := io.CopyN(f, tr, h.Size)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("archive links and special files are forbidden")
		}
	}
	// Consume the gzip trailer so truncation and CRC failures cannot pass validation.
	if _, e = io.Copy(io.Discard, gz); e != nil {
		return e
	}
	if count == 0 {
		return errors.New("empty archive")
	}
	return nil
}
func (a *App) InstallLocal(ctx context.Context) error {
	if !a.updateMu.TryLock() {
		return failure("busy", "another update is running", 409)
	}
	defer a.updateMu.Unlock()
	c := a.Config()
	if c.LocalDir == "" {
		return errors.New("configure local_dir first")
	}
	a.phase("copying")
	stage, e := os.MkdirTemp(a.stageRoot(), "local-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e = copyTree(c.LocalDir, filepath.Join(stage, "repo")); e != nil {
		return e
	}
	v, e := hashTree(filepath.Join(stage, "repo"))
	if e != nil {
		return e
	}
	if existing, loadErr := a.loadSnapshot(filepath.Join(a.Data, "versions", v), v); loadErr == nil {
		if e = a.prepareCurrentPython(ctx, existing); e != nil {
			return e
		}
		if e = a.validateTransition(ctx, existing); e != nil {
			return e
		}
		return a.activate(existing, "local-reuse")
	}
	return a.prepareAndActivate(ctx, stage, v, "local content SHA-256 prefix")
}
func (a *App) prepareAndActivate(ctx context.Context, stage, version, source string) (err error) {
	a.phase("validating")
	manifest := Manifest{Version: version, Engine: engine.Version, Source: source, Created: a.now(), Packages: []string{}}
	s, e := loadRegistry(filepath.Join(stage, "repo"), version, manifest)
	if e != nil {
		a.reject(version, e)
		return e
	}
	if e = a.preparePython(ctx, stage, version, s); e != nil {
		a.reject(version, e)
		return e
	}
	manifest = s.Manifest
	if e = a.validateTransition(ctx, s); e != nil {
		a.reject(version, e)
		return e
	}
	if manifest.Python != "" {
		manifest.Python = pythonIn(filepath.Join(a.Data, "versions", version, "runtime"))
	}
	if e = writeJSON(filepath.Join(stage, "manifest.json"), manifest); e != nil {
		return e
	}
	a.phase("switching")
	dest := filepath.Join(a.Data, "versions", version)
	if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return e
	}
	if _, e = os.Stat(dest); e == nil {
		return errors.New("snapshot directory already exists; refusing to overwrite immutable files")
	}
	if e = os.Rename(stage, dest); e != nil {
		return e
	}
	s, e = a.loadSnapshot(dest, version)
	if e != nil {
		return e
	}
	if e = a.activate(s, "apply"); e != nil {
		return e
	}
	return nil
}
func (a *App) reject(version string, err error) {
	a.mu.Lock()
	a.state.Failed[version] = err.Error()
	a.state.History = append(a.state.History, Event{Version: version, Action: "validate", Result: "failed", Reason: err.Error(), At: a.now(), Engine: engine.Version})
	a.state.Phase = "failed"
	a.state.Failure = err.Error()
	_ = a.persistState()
	a.mu.Unlock()
}
func (a *App) validateTransition(ctx context.Context, s *Snapshot) error {
	if err := runRegression(ctx, s); err != nil {
		return fmt.Errorf("offline regression failed: %w", err)
	}
	c := a.Config()
	old, done := a.acquire()
	defer done()
	if old != nil {
		for id, before := range old.Entries {
			after, exists := s.Entries[id]
			if !exists {
				continue
			} // deletion is represented as a tombstone, never served from the old version
			prev := a.describe(before, old, c)
			if prev.Enabled && before.rule != nil && after.rule == nil {
				return fmt.Errorf("enabled scraper %s no longer loads: %s", id, after.Reason)
			}
			if !prev.Enabled || before.rule == nil || after.rule == nil {
				continue
			}
			next := a.describe(after, s, c)
			for _, op := range prev.Operations {
				if op.State != "available" {
					continue
				}
				found := false
				for _, n := range next.Operations {
					if n.Object == op.Object && n.Operation.Operation == op.Operation.Operation && n.State == "available" {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("enabled action %s/%s/%s lost runtime compatibility", id, op.Object, op.Operation.Operation)
				}
			}
		}
	}
	available := 0
	for _, e := range s.Entries {
		if e.rule != nil {
			available++
		}
	}
	if available == 0 {
		return errors.New("no loadable scraper in candidate")
	}
	return nil
}
func (a *App) activate(s *Snapshot, action string) error {
	a.snapMu.Lock()
	defer a.snapMu.Unlock()
	a.mu.Lock()
	var oldState UpdateState
	b, _ := json.Marshal(a.state)
	_ = json.Unmarshal(b, &oldState)
	a.state.Active = s.Version
	a.state.LastSuccess = a.now()
	a.state.Phase = "idle"
	a.state.Failure = ""
	a.state.History = append(a.state.History, Event{Version: s.Version, Action: action, Result: "ok", At: a.now(), Engine: engine.Version, Dependencies: &s.Manifest})
	if len(a.state.History) > 100 {
		a.state.History = a.state.History[len(a.state.History)-100:]
	}
	if err := a.persistState(); err != nil {
		a.state = oldState
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	a.snapshots[s.Version] = s
	a.current.Store(s)
	a.pruneLocked()
	return nil
}
func (a *App) pruneLocked() {
	c := a.Config()
	dirs, e := os.ReadDir(filepath.Join(a.Data, "versions"))
	if e != nil {
		return
	}
	type version struct {
		path, sha string
		at        time.Time
	}
	var all []version
	for _, d := range dirs {
		if !d.IsDir() || !shaPattern.MatchString(d.Name()) {
			continue
		}
		full := filepath.Join(a.Data, "versions", d.Name())
		st, e := d.Info()
		if e == nil {
			all = append(all, version{full, d.Name(), st.ModTime()})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	kept := 0
	active := a.current.Load().Version
	for _, v := range all {
		if v.sha == active || v.sha == c.Update.Pin {
			continue
		}
		kept++
		if kept < c.Update.Retain {
			continue
		}
		if a.references[v.sha] > 0 {
			continue
		}
		_ = os.RemoveAll(v.path)
		_ = os.RemoveAll(filepath.Join(a.Data, "runtimes", v.sha))
		delete(a.snapshots, v.sha)
	}
}
func (a *App) Rollback(ctx context.Context, version string) error {
	if !a.updateMu.TryLock() {
		return failure("busy", "another update is running", 409)
	}
	defer a.updateMu.Unlock()
	if version == "" {
		s := a.State()
		for i := len(s.History) - 1; i >= 0; i-- {
			h := s.History[i]
			if h.Result == "ok" && h.Version != s.Active {
				if _, e := os.Stat(filepath.Join(a.Data, "versions", h.Version, "manifest.json")); e == nil {
					version = h.Version
					break
				}
			}
		}
	}
	if !shaPattern.MatchString(version) {
		return failure("not_found", "no retained rollback version", 404)
	}
	s, e := a.loadSnapshot(filepath.Join(a.Data, "versions", version), version)
	if e != nil {
		return e
	}
	if e = runRegression(ctx, s); e != nil {
		return e
	}
	c := a.Config()
	c.Update.Pin = version
	c.Update.PinUntil = a.now().Add(24 * time.Hour)
	if e = a.SaveConfig(c); e != nil {
		return e
	}
	return a.activate(s, "rollback")
}
func (a *App) Reload(ctx context.Context) error {
	if !a.updateMu.TryLock() {
		return failure("busy", "another update is running", 409)
	}
	defer a.updateMu.Unlock()
	c, e := loadConfig(filepath.Join(a.Data, "config.json"))
	if e != nil {
		return e
	}
	if e = a.SaveConfig(c); e != nil {
		return e
	}
	s, done := a.acquire()
	defer done()
	if s == nil {
		return errors.New("no installed snapshot")
	}
	fresh, e := a.loadSnapshot(filepath.Dir(s.Root), s.Version)
	if e != nil {
		return e
	}
	if e = runRegression(ctx, fresh); e != nil {
		return e
	}
	if e = a.prepareCurrentPython(ctx, fresh); e != nil {
		a.mu.Lock()
		a.state.Phase = "failed"
		a.state.Failure = "snapshot dependency validation failed; active runtime retained"
		a.state.History = append(a.state.History, Event{Version: s.Version, Action: "dependencies", Result: "failed", Reason: e.Error(), At: a.now(), Engine: engine.Version})
		_ = a.persistState()
		a.mu.Unlock()
		return e
	}
	a.phase("idle")
	a.snapMu.Lock()
	a.snapshots[s.Version] = fresh
	a.current.Store(fresh)
	a.snapMu.Unlock()
	return nil
}
