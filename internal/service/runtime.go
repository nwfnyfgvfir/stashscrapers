package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"stashscrapers/internal/engine"
)

func pythonIn(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "Scripts", "python.exe")
	}
	return filepath.Join(dir, "bin", "python")
}

type cappedBuffer struct {
	mu       sync.Mutex
	b        bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.b.Len()+len(p) > b.limit {
		b.overflow = true
		b.cancel()
		return 0, errors.New("process output limit exceeded")
	}
	return b.b.Write(p)
}
func runProcess(ctx context.Context, executable string, args []string, dir string, env []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &cappedBuffer{limit: 2 << 20, cancel: cancel}
	stderr := &cappedBuffer{limit: 256 << 10, cancel: cancel}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if out.overflow || stderr.overflow {
		return nil, failure("parse_error", "script output exceeded the configured safety limit", 502)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, failure("script_error", "script failed; stdout/stderr are withheld to protect credentials", 502)
	}
	return out.b.Bytes(), nil
}
func scriptEnv(python, root, proxy string) []string {
	env := []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "HOME", "LANG", "SSL_CERT_FILE", "REQUESTS_CA_BUNDLE"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, "PATH="+filepath.Dir(python)+string(os.PathListSeparator)+os.Getenv("PATH"), "PYTHONPATH="+root, "PYTHONUTF8=1", "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1", "PIP_DISABLE_PIP_VERSION_CHECK=1")
	if proxy != "" {
		env = append(env, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "http_proxy="+proxy, "https_proxy="+proxy)
	}
	return env
}
func (a *App) preparePython(ctx context.Context, stage, version string, s *Snapshot) error {
	c := a.Config()
	setting := c.Scrapers["RealjamVR/RealjamVR"]
	if !setting.ScriptApproved || c.PythonBase == "" {
		return nil
	}
	entry := s.Entries["RealjamVR/RealjamVR"]
	if entry == nil || entry.rule == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	a.phase("dependencies")
	dir := filepath.Join(stage, "runtime")
	env := scriptEnv(c.PythonBase, filepath.Join(s.Root, "scrapers"), c.Proxy)
	if _, e := runProcess(ctx, c.PythonBase, []string{"-m", "venv", dir}, s.Root, env, nil); e != nil {
		return errors.New("could not create isolated snapshot Python environment")
	}
	python := pythonIn(dir)
	env = scriptEnv(python, filepath.Join(s.Root, "scrapers"), c.Proxy)
	// Exact pins are maintained separately from upstream files. Never upgrade a shared environment.
	packages := []string{"requests==2.32.5", "urllib3==2.5.0", "idna==3.10", "charset-normalizer==3.4.3", "certifi==2025.8.3"}
	args := append([]string{"-m", "pip", "install", "--no-input", "--disable-pip-version-check"}, packages...)
	if _, e := runProcess(ctx, python, args, s.Root, env, nil); e != nil {
		return errors.New("isolated Python package installation failed; old snapshot retained")
	}
	var script string
	for _, op := range entry.rule.Operations() {
		if op.Action == "script" && len(op.Script) > 1 {
			script = op.Script[1]
			break
		}
	}
	if script == "" {
		return errors.New("audited Python script is missing")
	}
	probe := "import sys, runpy; assert sys.version_info >= (3, 12); runpy.run_path(sys.argv[1], run_name='compatibility_probe'); print(sys.version.split()[0])"
	output, e := runProcess(ctx, python, []string{"-c", probe, filepath.Join(filepath.Dir(entry.path), script)}, filepath.Dir(entry.path), env, nil)
	if e != nil {
		return errors.New("Python syntax/import validation failed")
	}
	// Validate the staged interpreter; assign its final path after validation.
	s.Manifest.Python = python
	s.Manifest.PythonVersion = strings.TrimSpace(string(output))
	s.Manifest.Packages = packages
	if e = pythonOfflineRegression(ctx, python, entry, s); e != nil {
		return e
	}
	return nil
}
func pythonOfflineRegression(ctx context.Context, python string, entry *Entry, s *Snapshot) error {
	// Only the regression subprocess receives this fixture network adapter. Production executes the installed requests package.
	dir, e := os.MkdirTemp("", "scraper-python-fixture-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	stub := "import json\nclass Response:\n def json(self):\n  return json.loads(" + fmt.Sprintf("%q", fixtureBody("realjam-search.json")) + ")\ndef post(url, data):\n assert url == 'https://realjamvr.com/search/'\n assert data == {'search': 'fixture query'}\n return Response()\n"
	if e = os.WriteFile(filepath.Join(dir, "requests.py"), []byte(stub), 0600); e != nil {
		return e
	}
	env := scriptEnv(python, dir+string(os.PathListSeparator)+filepath.Join(s.Root, "scrapers"), "")
	var args []string
	for _, op := range entry.rule.Operations() {
		if op.Action == "script" {
			args = op.Script[1:]
			break
		}
	}
	out, e := runProcess(ctx, python, args, filepath.Dir(entry.path), env, []byte(`{"name":"fixture query"}`))
	if e != nil {
		return errors.New("Python script protocol fixture failed")
	}
	var results []map[string]interface{}
	if json.Unmarshal(out, &results) != nil || len(results) != 2 || results[0]["title"] != "Script fixture one" {
		return errors.New("Python script result contract changed")
	}
	return nil
}
func (a *App) prepareCurrentPython(ctx context.Context, s *Snapshot) error {
	c := a.Config()
	if s.Manifest.Python != "" || c.PythonBase == "" || !c.Scrapers["RealjamVR/RealjamVR"].ScriptApproved {
		return nil
	}
	stage, e := os.MkdirTemp(a.stageRoot(), "runtime-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e = a.preparePython(ctx, stage, s.Version, s); e != nil {
		return e
	}
	if s.Manifest.Python == "" {
		return nil
	}
	parent := filepath.Join(a.Data, "runtimes", s.Version)
	if e = os.MkdirAll(parent, 0700); e != nil {
		return e
	}
	dest := filepath.Join(parent, fmt.Sprintf("%d", time.Now().UnixNano()))
	if e = os.Rename(stage, dest); e != nil {
		return e
	}
	s.Manifest.Python = pythonIn(filepath.Join(dest, "runtime"))
	return writeJSON(filepath.Join(a.Data, "versions", s.Version, "manifest.json"), s.Manifest)
}
func (a *App) scriptRunner(s *Snapshot, e *Entry, c Config) engine.ScriptFunc {
	return func(ctx context.Context, args []string, input map[string]interface{}) (interface{}, error) {
		if e.ID != "RealjamVR/RealjamVR" || len(args) < 2 || !c.Scrapers[e.ID].ScriptApproved {
			return nil, failure("unsupported", "script has no standalone execution approval", 422)
		}
		if !c.AllowPrivateTargets {
			return nil, failure("needs_configuration", "this audited Python runtime requires allow_private_targets acknowledgement because Python manages its own network transport", 424)
		}
		if s.Manifest.Python == "" {
			return nil, failure("missing_dependency", "snapshot Python runtime is not installed", 424)
		}
		if args[0] != "python" && args[0] != "python3" {
			return nil, failure("unsupported", "only an explicitly configured Python interpreter may execute scripts", 422)
		}
		base := filepath.Dir(e.path)
		script := filepath.Join(base, args[1])
		relative, err := filepath.Rel(base, script)
		if err != nil || filepath.IsAbs(args[1]) || strings.HasPrefix(relative, "..") {
			return nil, failure("input_error", "script path escapes its trusted directory", 400)
		}
		body, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		if err = a.limiter.wait(ctx, "realjamvr.com", c.SiteIntervalMS); err != nil {
			return nil, err
		}
		out, err := runProcess(ctx, s.Manifest.Python, args[1:], base, scriptEnv(s.Manifest.Python, filepath.Join(s.Root, "scrapers"), c.Proxy), body)
		if err != nil {
			return nil, err
		}
		var result interface{}
		dec := json.NewDecoder(bytes.NewReader(out))
		if err = dec.Decode(&result); err != nil {
			return nil, failure("parse_error", "script stdout is not valid JSON", 502)
		}
		var extra interface{}
		if dec.Decode(&extra) != io.EOF {
			return nil, failure("parse_error", "script stdout must contain exactly one JSON result", 502)
		}
		return result, nil
	}
}
func (a *App) DependencyStatus() map[string]interface{} {
	c := a.Config()
	s, done := a.acquire()
	defer done()
	ret := map[string]interface{}{"engine": engine.Version, "python_configured": c.PythonBase != "", "cdp_configured": c.CDP != "", "cdp_verified": false, "notes": []string{"CDP connectivity and target website availability require explicit execution"}}
	if s != nil {
		ret["snapshot"] = s.Manifest
	}
	return ret
}
