package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestGitHubGlobalPAXHeader(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if e := tw.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": v1}}); e != nil {
		t.Fatal(e)
	}
	data := []byte("original resource")
	_ = tw.WriteHeader(&tar.Header{Name: "repo/scrapers/resource.txt", Size: int64(len(data)), Mode: 0600})
	_, _ = tw.Write(data)
	_ = tw.Close()
	_ = gz.Close()
	dir := t.TempDir()
	if e := extractArchive(&b, dir); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(filepath.Join(dir, "scrapers/resource.txt"))
	if e != nil || !bytes.Equal(got, data) {
		t.Fatal("GitHub archive data lost", e)
	}
}
