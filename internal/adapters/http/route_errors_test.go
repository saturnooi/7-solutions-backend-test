package http

import (
	"context"
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
