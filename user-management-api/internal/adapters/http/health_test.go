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
		cancelled  bool
		check      func(context.Context) error
		wantStatus int
		wantBody   string
		wantError  string
		wantInfo   string
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
			wantError:  "server selection timeout",
		},
		{
			name:       "probe gave up before the ping finished",
			cancelled:  true,
			check:      func(ctx context.Context) error { return ctx.Err() },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
			wantInfo:   "health check canceled",
		},
		{
			name:       "probe gave up but the ping failed for another reason",
			cancelled:  true,
			check:      func(context.Context) error { return errors.New("mongodb: ping: server selection timeout") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
			wantError:  "server selection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			health := &HealthCheckerMock{CheckFunc: tt.check}
			var logs bytes.Buffer
			router := NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, health, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/healthz", nil)
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
			if tt.wantError == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
			}
			if tt.wantError != "" && !strings.Contains(errorLogs(&logs), tt.wantError) {
				t.Errorf("error log = %s, want it to contain %q", errorLogs(&logs), tt.wantError)
			}
			if tt.wantInfo != "" && !strings.Contains(logs.String(), `"msg":"`+tt.wantInfo+`"`) {
				t.Errorf("log = %s, want an INFO %q line", logs.String(), tt.wantInfo)
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
