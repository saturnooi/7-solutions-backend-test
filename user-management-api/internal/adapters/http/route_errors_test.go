package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"user-management-api/internal/domain/user"
)

func TestRoutes_ErrorsAreJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{
			name:       "unknown path",
			method:     http.MethodGet,
			target:     "/api/v1/nowhere",
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"no route matches this path"}}`,
		},
		{
			name:       "users path without an id",
			method:     http.MethodGet,
			target:     "/api/v1/users/",
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"no route matches this path"}}`,
		},
		{
			name:       "wrong method keeps the Allow header",
			method:     http.MethodPut,
			target:     "/api/v1/users/" + callerID,
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   `{"error":{"code":"method_not_allowed","message":"this path does not accept the method"}}`,
			wantAllow:  "DELETE, GET, HEAD, PATCH",
		},
		{
			name:       "matched route is untouched",
			method:     http.MethodGet,
			target:     "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "a matched handler's own not found keeps its body",
			method:     http.MethodGet,
			target:     "/api/v1/users/" + callerID,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"user_not_found","message":"user not found"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			health := &HealthCheckerMock{CheckFunc: func(context.Context) error { return nil }}
			service := &UserServiceMock{GetUserFunc: func(context.Context, string) (user.User, error) { return user.User{}, user.ErrNotFound }}
			accept := &AuthenticatorMock{AuthenticateFunc: func(context.Context, string) (string, error) { return callerID, nil }}
			router := NewHandler(service, accept, health, slog.New(slog.DiscardHandler)).Routes()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
			req.Header.Set("Authorization", "Bearer valid.token.signature")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}

func TestRouteErrorWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		body     string
		wantBody string
	}{
		{
			name:     "router not found is replaced",
			status:   http.StatusNotFound,
			body:     "404 page not found\n",
			wantBody: `{"error":{"code":"not_found","message":"no route matches this path"}}` + "\n",
		},
		{
			name:     "router method not allowed is replaced",
			status:   http.StatusMethodNotAllowed,
			body:     "Method Not Allowed\n",
			wantBody: `{"error":{"code":"method_not_allowed","message":"this path does not accept the method"}}` + "\n",
		},
		{
			name:     "other router responses are untouched",
			status:   http.StatusMovedPermanently,
			body:     "<a href=\"/x\">Moved Permanently</a>.\n",
			wantBody: "<a href=\"/x\">Moved Permanently</a>.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			w := &routeErrorWriter{ResponseWriter: rec}

			w.WriteHeader(tt.status)
			n, err := w.Write([]byte(tt.body))

			if err != nil || n != len(tt.body) {
				t.Errorf("Write() = (%d, %v), want (%d, nil)", n, err, len(tt.body))
			}
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if w.Unwrap() != rec {
				t.Error("Unwrap() does not return the underlying writer")
			}
		})
	}
}

func TestRoutes_OversizedBodyClosesTheConnection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, &HealthCheckerMock{}, slog.New(slog.DiscardHandler)).Routes())
	t.Cleanup(server.Close)
	body := `{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/auth/register", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if !resp.Close {
		t.Error("response keeps the connection open, want Connection: close after an oversized body")
	}
}

func TestServiceError_Cancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cancelled  bool
		cause      error
		wantStatus int
		wantCode   string
		wantLog    string
	}{
		{
			name:       "client gave up and the call stopped because of it",
			cancelled:  true,
			cause:      context.Canceled,
			wantStatus: statusClientClosedRequest,
			wantCode:   "request_canceled",
			wantLog:    `"level":"INFO","msg":"request canceled"`,
		},
		{
			name:       "client gave up but the call failed for another reason",
			cancelled:  true,
			cause:      errors.New("write concern timeout"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantLog:    `"level":"ERROR","msg":"request failed"`,
		},
		{
			name:       "client gave up while the database timed out on its own",
			cancelled:  true,
			cause:      context.DeadlineExceeded,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantLog:    `"level":"ERROR","msg":"request failed"`,
		},
		{
			name:       "a cancellation inside the server while the client still waits",
			cause:      context.Canceled,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantLog:    `"level":"ERROR","msg":"request failed"`,
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
			service := &UserServiceMock{GetUserFunc: func(context.Context, string) (user.User, error) {
				return user.User{}, fmt.Errorf("get user: mongodb: find user by id: %w", tt.cause)
			}}
			accept := &AuthenticatorMock{AuthenticateFunc: func(context.Context, string) (string, error) { return callerID, nil }}
			var logs bytes.Buffer
			router := NewHandler(service, accept, &HealthCheckerMock{}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/"+callerID, nil)
			req.Header.Set("Authorization", "Bearer valid.token.signature")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Errorf("body = %s, want code %q", rec.Body.String(), tt.wantCode)
			}
			if !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("logs = %s, want a line with %s", logs.String(), tt.wantLog)
			}
			if wantError := strings.Contains(tt.wantLog, `"ERROR"`); (errorLogs(&logs) != "") != wantError {
				t.Errorf("ERROR logged = %v, want %v (logs %s)", errorLogs(&logs) != "", wantError, logs.String())
			}
		})
	}
}
