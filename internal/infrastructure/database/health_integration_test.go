//go:build integration

package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHealth_Check(t *testing.T) {
	t.Parallel()

	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	tests := []struct {
		name       string
		disconnect bool
		wantErr    bool
	}{
		{name: "connected client", wantErr: false},
		{name: "disconnected client", disconnect: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := Connect(t.Context(), uri, 5*time.Second)
			if err != nil {
				t.Fatalf("connect to test MongoDB at %s (run `make db-up`): %v", uri, err)
			}
			if tt.disconnect {
				if err := client.Disconnect(t.Context()); err != nil {
					t.Fatalf("Disconnect() error = %v", err)
				}
			} else {
				t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
			}

			err = NewHealth(client).Check(t.Context())

			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, want error %v", err, tt.wantErr)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "mongodb: ping:") {
				t.Errorf("Check() error = %q, want a wrapped ping error", err)
			}
		})
	}
}
