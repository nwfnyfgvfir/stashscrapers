package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContainerHealthProbe(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		healthy    bool
	}{
		{"initializing", `{"status":"ok","ready":false}`, 200, true},
		{"ready", `{"status":"ok","ready":true}`, 200, true},
		{"unhealthy", `{"status":"failed"}`, 200, false},
		{"error", `{}`, 503, false},
		{"invalid JSON", `not JSON`, 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			if err := checkHealth(server.URL); (err == nil) != test.healthy {
				t.Fatal("unexpected health result", err)
			}
		})
	}
}
