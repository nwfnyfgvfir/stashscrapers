package service

import (
	"net"
	"net/http"
	"os"
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
	server := &http.Server{Handler: a.Handler()}
	go server.Serve(listener)
	defer server.Close()
	t.Log("Controlled fixture UI: http://127.0.0.1:18081 (test data only)")
	time.Sleep(10 * time.Minute)
}
