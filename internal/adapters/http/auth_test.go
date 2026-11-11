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

	"user-management-api/internal/application/userservice"
)

const unauthorizedBody = `{"error":{"code":"unauthorized","message":"a valid bearer token is required"}}`

func TestRequireAuth(t *testing.T) {
	t.Parallel()

	const userID = "6730f1c2a1b2c3d4e5f60718"
	accept := func(context.Context, string) (string, error) { return userID, nil }
	reject := func(context.Context, string) (string, error) {
		return "", fmt.Errorf("%w: jwt: invalid token: token is expired", userservice.ErrUnauthenticated)
	}

	tests := []struct {
		name          string
		authorization string
		authenticate  func(context.Context, string) (string, error)
		wantStatus    int
		wantChallenge string
		wantBody      string
		wantToken     string
		wantLog       string
	}{
		{
			name:          "valid bearer token",
			authorization: "Bearer header.payload.signature",
			authenticate:  accept,
			wantStatus:    http.StatusNoContent,
			wantToken:     "header.payload.signature",
		},
		{
			name:          "scheme is case-insensitive",
			authorization: "bearer header.payload.signature",
			authenticate:  accept,
			wantStatus:    http.StatusNoContent,
			wantToken:     "header.payload.signature",
		},
		{
			name:          "spaces around the token are ignored",
			authorization: "Bearer   header.payload.signature  ",
			authenticate:  accept,
			wantStatus:    http.StatusNoContent,
			wantToken:     "header.payload.signature",
		},
		{
			name:          "no Authorization header",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
			wantBody:      unauthorizedBody,
		},
		{
			name:          "basic credentials",
			authorization: "Basic YWxpY2U6c2VjcmV0",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
			wantBody:      unauthorizedBody,
		},
		{
			name:          "scheme without a token",
			authorization: "Bearer",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
			wantBody:      unauthorizedBody,
		},
		{
			name:          "scheme with a blank token",
			authorization: "Bearer    ",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
			wantBody:      unauthorizedBody,
		},
		{
			name:          "token without a scheme",
			authorization: "header.payload.signature",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
			wantBody:      unauthorizedBody,
		},
		{
			name:          "token rejected",
			authorization: "Bearer expired.payload.signature",
			authenticate:  reject,
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: `Bearer error="invalid_token"`,
			wantBody:      unauthorizedBody,
			wantToken:     "expired.payload.signature",
		},
		{
			name:          "lookup failure is a server error, not a rejection",
			authorization: "Bearer header.payload.signature",
			authenticate: func(context.Context, string) (string, error) {
				return "", errors.New("authenticate: mongodb: find user by id: server selection timeout")
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantToken:  "header.payload.signature",
			wantLog:    "server selection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			auth := &AuthenticatorMock{AuthenticateFunc: tt.authenticate}
			var logs bytes.Buffer
			h := NewHandler(&UserServiceMock{}, auth, slog.New(slog.NewJSONHandler(&logs, nil)))
			var nextCalled bool
			var seenUserID string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				seenUserID, _ = userIDFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()

			h.requireAuth(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tt.wantChallenge {
				t.Errorf("WWW-Authenticate = %q, want %q", got, tt.wantChallenge)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
			if tt.wantStatus == http.StatusNoContent {
				if seenUserID != userID {
					t.Errorf("user ID in context = %q, want %q", seenUserID, userID)
				}
			} else if nextCalled {
				t.Error("next handler ran for a request that failed authentication")
			}

			calls := auth.AuthenticateCalls()
			switch {
			case tt.wantToken == "" && len(calls) != 0:
				t.Errorf("Authenticate calls = %d, want 0", len(calls))
			case tt.wantToken != "" && (len(calls) != 1 || calls[0].Token != tt.wantToken):
				t.Errorf("Authenticate calls = %+v, want one call with %q", calls, tt.wantToken)
			}

			if tt.wantLog == "" && logs.Len() > 0 {
				t.Errorf("unexpected log output: %s", logs.String())
			}
			if tt.wantLog != "" && !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %s, want it to contain %q", logs.String(), tt.wantLog)
			}
		})
	}
}

func TestUserIDFromContext_WithoutAuthentication(t *testing.T) {
	t.Parallel()

	got, ok := userIDFromContext(t.Context())

	if ok || got != "" {
		t.Errorf("userIDFromContext() = (%q, %v), want (\"\", false)", got, ok)
	}
}
