package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

func TestLogRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		next       http.HandlerFunc
		wantStatus float64
		wantPath   string
	}{
		{
			name:       "explicit status",
			method:     http.MethodPost,
			target:     "/things",
			next:       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) },
			wantStatus: http.StatusCreated,
			wantPath:   "/things",
		},
		{
			name:       "body without WriteHeader is a 200",
			method:     http.MethodGet,
			target:     "/things",
			next:       func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
			wantStatus: http.StatusOK,
			wantPath:   "/things",
		},
		{
			name:       "nothing written is a 200",
			method:     http.MethodGet,
			target:     "/things",
			next:       func(http.ResponseWriter, *http.Request) {},
			wantStatus: http.StatusOK,
			wantPath:   "/things",
		},
		{
			name:   "a status after the body is ignored, as net/http ignores it",
			method: http.MethodGet,
			target: "/things",
			next: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("ok"))
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: http.StatusOK,
			wantPath:   "/things",
		},
		{
			name:   "the first status wins",
			method: http.MethodDelete,
			target: "/things/1",
			next: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.WriteHeader(http.StatusOK)
			},
			wantStatus: http.StatusNotFound,
			wantPath:   "/things/1",
		},
		{
			name:       "query string is not logged",
			method:     http.MethodGet,
			target:     "/things?limit=5&token=secret-value",
			next:       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			wantStatus: http.StatusOK,
			wantPath:   "/things",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			h := NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, slog.New(slog.NewJSONHandler(&logs, nil)))
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
			rec := httptest.NewRecorder()

			h.logRequests(tt.next).ServeHTTP(rec, req)

			entry := onlyRequestLog(t, &logs)
			if entry["level"] != "INFO" || entry["method"] != tt.method || entry["path"] != tt.wantPath || entry["status"] != tt.wantStatus {
				t.Errorf("log = %v, want INFO %s %s %v", entry, tt.method, tt.wantPath, tt.wantStatus)
			}
			if _, ok := entry["duration_ms"].(float64); !ok {
				t.Errorf("duration_ms = %v, want a number", entry["duration_ms"])
			}
			if strings.Contains(logs.String(), "secret-value") {
				t.Errorf("log contains the query string: %s", logs.String())
			}
		})
	}
}

func TestLogRequests_KeepsResponseControllerWorking(t *testing.T) {
	t.Parallel()

	h := NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, slog.New(slog.DiscardHandler))
	var flushErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flushErr = http.NewResponseController(w).Flush()
	})
	rec := httptest.NewRecorder()

	h.logRequests(next).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", nil))

	if flushErr != nil {
		t.Fatalf("Flush() through the logging wrapper error = %v, want nil", flushErr)
	}
	if !rec.Flushed {
		t.Error("the underlying writer was not flushed")
	}
}

func TestLogRequests_MeasuresDuration(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		h := NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, slog.New(slog.NewJSONHandler(&logs, nil)))
		slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/slow", nil)

		h.logRequests(slow).ServeHTTP(httptest.NewRecorder(), req)

		if got := onlyRequestLog(t, &logs)["duration_ms"]; got != 150.0 {
			t.Errorf("duration_ms = %v, want 150", got)
		}
	})
}

func TestRoutes_LogEveryRequest(t *testing.T) {
	t.Parallel()

	const token = "eyJhbGciOiJIUzI1NiJ9.secret-token.signature"
	register := func(context.Context, userservice.RegisterInput) (user.User, error) { return created, nil }

	tests := []struct {
		name          string
		method        string
		target        string
		body          string
		authorization string
		wantStatus    float64
		wantPath      string
	}{
		{
			name:       "registration",
			method:     http.MethodPost,
			target:     "/api/v1/auth/register",
			body:       `{"name":"Bob Jones","email":"bob@example.com","password":"correct horse"}`,
			wantStatus: http.StatusCreated,
			wantPath:   "/api/v1/auth/register",
		},
		{
			name:          "protected route with a rejected token",
			method:        http.MethodGet,
			target:        "/api/v1/users?limit=5",
			authorization: "Bearer " + token,
			wantStatus:    http.StatusUnauthorized,
			wantPath:      "/api/v1/users",
		},
		{
			name:       "unknown path",
			method:     http.MethodGet,
			target:     "/nowhere",
			wantStatus: http.StatusNotFound,
			wantPath:   "/nowhere",
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			target:     "/api/v1/auth/login",
			wantStatus: http.StatusMethodNotAllowed,
			wantPath:   "/api/v1/auth/login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reject := func(context.Context, string) (string, error) { return "", userservice.ErrUnauthenticated }
			var logs bytes.Buffer
			router := NewHandler(
				&UserServiceMock{RegisterFunc: register},
				&AuthenticatorMock{AuthenticateFunc: reject},
				slog.New(slog.NewJSONHandler(&logs, nil)),
			).Routes()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, strings.NewReader(tt.body))
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			entry := onlyRequestLog(t, &logs)
			if entry["method"] != tt.method || entry["path"] != tt.wantPath || entry["status"] != tt.wantStatus {
				t.Errorf("log = %v, want %s %s %v", entry, tt.method, tt.wantPath, tt.wantStatus)
			}
			if float64(rec.Code) != tt.wantStatus {
				t.Errorf("response status = %d, want %v", rec.Code, tt.wantStatus)
			}
			for _, secret := range []string{token, "correct horse", "bob@example.com"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("log contains %q: %s", secret, logs.String())
				}
			}
		})
	}
}

func onlyRequestLog(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()

	var found []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if entry["msg"] == "http request" {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		t.Fatalf("request log lines = %d, want 1 (logs %s)", len(found), logs.String())
	}
	return found[0]
}

func errorLogs(logs *bytes.Buffer) string {
	var out strings.Builder
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"level":"ERROR"`) {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}
