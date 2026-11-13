package service

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

// Test-only fixture HTTP service for browser end-to-end verification. It is never
// linked into the production command or enabled by production configuration.
func TestUIFixtureHarness(t *testing.T) {
	if os.Getenv("TEST_UI_LAB") != "1" {
		t.Skip("manual browser verification harness")
	}
	a := fixtureApp(t)
	controlledTarget(t, a)
	listener, e := net.Listen("tcp", "127.0.0.1:18081")
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__test__/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		once.Do(func() { close(done) })
	})
	mux.Handle("/", a.Handler())
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()
	t.Log("Controlled fixture UI: http://127.0.0.1:18081 (test data only)")
	select {
	case <-done:
	case <-time.After(10 * time.Minute):
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
