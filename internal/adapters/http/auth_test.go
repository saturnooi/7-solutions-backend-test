package http

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const unauthorizedBody = `{"error":{"code":"unauthorized","message":"a valid bearer token is required"}}`

func TestRequireAuth(t *testing.T) {
	t.Parallel()

	const userID = "6730f1c2a1b2c3d4e5f60718"
	accept := func(string) (string, error) { return userID, nil }
	reject := func(string) (string, error) { return "", errors.New("jwt: invalid token: token is expired") }

	tests := []struct {
		name          string
		authorization string
		verify        func(string) (string, error)
		wantStatus    int
		wantChallenge string
		wantVerified  string
	}{
		{
			name:          "valid bearer token",
			authorization: "Bearer header.payload.signature",
			verify:        accept,
			wantStatus:    http.StatusNoContent,
			wantVerified:  "header.payload.signature",
		},
		{
			name:          "scheme is case-insensitive",
			authorization: "bearer header.payload.signature",
			verify:        accept,
			wantStatus:    http.StatusNoContent,
			wantVerified:  "header.payload.signature",
		},
		{
			name:          "spaces around the token are ignored",
			authorization: "Bearer   header.payload.signature  ",
			verify:        accept,
			wantStatus:    http.StatusNoContent,
			wantVerified:  "header.payload.signature",
		},
		{
			name:          "no Authorization header",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
		},
		{
			name:          "basic credentials",
			authorization: "Basic YWxpY2U6c2VjcmV0",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
		},
		{
			name:          "scheme without a token",
			authorization: "Bearer",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
		},
		{
			name:          "scheme with a blank token",
			authorization: "Bearer    ",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
		},
		{
			name:          "token without a scheme",
			authorization: "header.payload.signature",
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: "Bearer",
		},
		{
			name:          "token rejected by the verifier",
			authorization: "Bearer expired.payload.signature",
			verify:        reject,
			wantStatus:    http.StatusUnauthorized,
			wantChallenge: `Bearer error="invalid_token"`,
			wantVerified:  "expired.payload.signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verifier := &TokenVerifierMock{VerifyFunc: tt.verify}
			h := NewHandler(&UserServiceMock{}, verifier, slog.New(slog.DiscardHandler))
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
			if tt.wantStatus == http.StatusUnauthorized {
				if nextCalled {
					t.Error("next handler ran for an unauthenticated request")
				}
				if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != unauthorizedBody {
					t.Errorf("body = %s, want %s", got, unauthorizedBody)
				}
			} else if seenUserID != userID {
				t.Errorf("user ID in context = %q, want %q", seenUserID, userID)
			}

			calls := verifier.VerifyCalls()
			switch {
			case tt.wantVerified == "" && len(calls) != 0:
				t.Errorf("Verify calls = %d, want 0", len(calls))
			case tt.wantVerified != "" && (len(calls) != 1 || calls[0].Token != tt.wantVerified):
				t.Errorf("Verify calls = %+v, want one call with %q", calls, tt.wantVerified)
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
