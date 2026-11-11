package http

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		check      func(context.Context) error
		wantStatus int
		wantBody   string
		wantLog    string
	}{
		{
			name:       "database reachable",
			check:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "database unreachable",
			check:      func(context.Context) error { return errors.New("mongodb: ping: server selection timeout") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
			wantLog:    "server selection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			health := &HealthCheckerMock{CheckFunc: tt.check}
			var logs bytes.Buffer
			router := NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, health, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), "mongodb") {
				t.Errorf("body exposes the cause: %s", rec.Body.String())
			}
			if tt.wantLog == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
			}
			if tt.wantLog != "" && !strings.Contains(errorLogs(&logs), tt.wantLog) {
				t.Errorf("error log = %s, want it to contain %q", errorLogs(&logs), tt.wantLog)
			}

			calls := health.CheckCalls()
			if len(calls) != 1 {
				t.Fatalf("Check calls = %d, want 1", len(calls))
			}
			deadline, ok := calls[0].Ctx.Deadline()
			if !ok || time.Until(deadline) > healthTimeout {
				t.Errorf("Check context deadline = %v (set %v), want at most %v from now", deadline, ok, healthTimeout)
			}
		})
	}
}
