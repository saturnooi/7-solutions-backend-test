package database

import (
	"strings"
	"testing"
	"time"
)

func TestConnect_Fails(t *testing.T) {
	t.Parallel()

	const password = "s3cret-pass"

	tests := []struct {
		name       string
		uri        string
		wantPrefix string
	}{
		{name: "unsupported scheme", uri: "http://localhost:27017", wantPrefix: "mongodb: connect:"},
		{name: "invalid port", uri: "mongodb://admin:" + password + "@localhost:notaport", wantPrefix: "mongodb: connect:"},
		{name: "unreachable server", uri: "mongodb://admin:" + password + "@127.0.0.1:1", wantPrefix: "mongodb: ping:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := Connect(t.Context(), tt.uri, 300*time.Millisecond)

			if err == nil {
				t.Fatal("Connect() error = nil, want an error")
			}
			if client != nil {
				t.Error("Connect() returned a client on error")
			}
			if !strings.HasPrefix(err.Error(), tt.wantPrefix) {
				t.Errorf("Connect() error = %q, want prefix %q", err, tt.wantPrefix)
			}
			if strings.Contains(err.Error(), password) {
				t.Errorf("Connect() error leaks the password: %q", err)
			}
		})
	}
}
