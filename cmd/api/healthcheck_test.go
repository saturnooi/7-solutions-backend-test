package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	t.Parallel()

	serve := func(status int) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(server.Close)
		return server.Listener.Addr().String()
	}
	healthy := serve(http.StatusOK)
	unhealthy := serve(http.StatusServiceUnavailable)
	_, healthyPort, err := net.SplitHostPort(healthy)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}

	tests := []struct {
		name    string
		addr    string
		wantErr string
	}{
		{name: "healthy server", addr: healthy},
		{name: "empty host means this machine", addr: ":" + healthyPort},
		{name: "0.0.0.0 means this machine", addr: "0.0.0.0:" + healthyPort},
		{name: "unhealthy server", addr: unhealthy, wantErr: "health check: status 503"},
		{name: "nothing listening", addr: "127.0.0.1:1", wantErr: "health check:"},
		{name: "not a host and port", addr: "localhost", wantErr: "health check:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := healthcheck(t.Context(), tt.addr)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("healthcheck() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Errorf("healthcheck() error = %v, want prefix %q", err, tt.wantErr)
			}
		})
	}
}
