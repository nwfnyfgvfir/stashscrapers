package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalPythonScriptOverControlledHTTPS(t *testing.T) {
	base := os.Getenv("TEST_PYTHON")
	if os.Getenv("TEST_SCRIPT_RUNTIME") != "1" || base == "" {
		t.Skip("set TEST_SCRIPT_RUNTIME=1 and TEST_PYTHON for isolated package/HTTPS integration")
	}
	source := t.TempDir()
	files := communityFiles(t)
	for _, name := range []string{"RealjamVR/RealjamVR.yml", "RealjamVR/RealJamVR.py", "py_common/util.py", "py_common/types.py", "py_common/log.py"} {
		b, e := fixtures.ReadFile("testdata/community/" + name)
		if e != nil {
			t.Fatal(e)
		}
		files["scrapers/"+name] = b
	}
	writeFiles(t, source, files)
	a, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := a.Config()
	c.Update.Enabled = false
	c.LocalDir = source
	c.AllowPrivateTargets = true
	c.SiteIntervalMS = 0
	c.PythonBase = base
	c.Scrapers["RealjamVR/RealjamVR"] = ScraperConfig{Enabled: true, ScriptApproved: true}
	if e = a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if e = a.InstallLocal(ctx); e != nil {
		t.Fatal(e)
	}
	snapshot := a.current.Load()
	if snapshot.Manifest.Python == "" || snapshot.Manifest.PythonVersion == "" || len(snapshot.Manifest.Packages) != 5 {
		t.Fatal("isolated dependency contract missing", snapshot.Manifest)
	}
	// The unmodified community requests.post() makes a genuine HTTPS request
	// through a controlled CONNECT proxy. Its target certificate is verified.
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "realjamvr.com"}, DNSNames: []string{"realjamvr.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pair, e := tls.X509KeyPair(certPEM, keyPEM)
	if e != nil {
		t.Fatal(e)
	}
	calls := make(chan string, 2)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/search/" {
			t.Error("upstream request semantics changed", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		calls <- r.FormValue("search")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, fixtureBody("realjam-search.json"))
	}))
	target.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	target.StartTLS()
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "realjamvr.com:443" {
			http.Error(w, "unapproved fixture target", 403)
			return
		}
		up, e := net.Dial("tcp", target.Listener.Addr().String())
		if e != nil {
			http.Error(w, "dial", 502)
			return
		}
		down, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			up.Close()
			return
		}
		_, _ = down.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { defer down.Close(); defer up.Close(); _, _ = io.Copy(up, down) }()
		_, _ = io.Copy(down, up)
	}))
	defer proxy.Close()
	caPath := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if e = os.WriteFile(caPath, certPEM, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("REQUESTS_CA_BUNDLE", caPath)
	c = a.Config()
	c.Proxy = proxy.URL
	if e = a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	result, e := a.Scrape(ctx, ScrapeRequest{ScraperID: "RealjamVR/RealjamVR", Object: "scene", Query: "fixture query"}, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Results) != 2 || result.Results[0].Title == nil || *result.Results[0].Title != "Script fixture one" {
		t.Fatal("script output protocol changed", result)
	}
	if <-calls != "fixture query" {
		t.Fatal("JSON stdin was not passed as search data")
	}
	if result.Results[1].URLs[0] != "https://realjamvr.com/scene/fixture-two" {
		t.Fatal("relative resources or script output mapped incorrectly")
	}
	t.Logf("validated Python %s, snapshot-bound packages: %v", snapshot.Manifest.PythonVersion, snapshot.Manifest.Packages)
	oldPython := snapshot.Manifest.Python
	oldVersion := snapshot.Version
	t.Setenv("REQUESTS_CA_BUNDLE", "")
	c = a.Config()
	c.Proxy = ""
	if e = a.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "resource-version.txt"), []byte("next snapshot"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = a.InstallLocal(ctx); e != nil {
		t.Fatal("upgrading an already enabled script failed", e)
	}
	if a.current.Load().Manifest.Python == oldPython || a.current.Load().Version == oldVersion {
		t.Fatal("Python dependencies were shared across snapshots")
	}
	if e = a.Rollback(ctx, oldVersion); e != nil {
		t.Fatal(e)
	}
	if a.current.Load().Manifest.Python != oldPython {
		t.Fatal("rollback did not restore its dependency environment")
	}
	if _, e = os.Stat(oldPython); e != nil {
		t.Fatal("old Python runtime was removed", e)
	}
}
