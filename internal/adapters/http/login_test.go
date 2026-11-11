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

	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

const loginPath = "/api/v1/auth/login"

func TestLogin(t *testing.T) {
	t.Parallel()

	const (
		validBody = `{"email":"alice@example.com","password":"correct horse"}`
		jwtValue  = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature"
	)

	tests := []struct {
		name        string
		body        string
		login       func(context.Context, userservice.LoginInput) (userservice.AccessToken, error)
		wantStatus  int
		wantBody    string
		wantNoStore bool
		wantCalls   int
		wantLog     string
	}{
		{
			name: "token issued",
			body: validBody,
			login: func(context.Context, userservice.LoginInput) (userservice.AccessToken, error) {
				return userservice.AccessToken{Value: jwtValue, ExpiresIn: time.Hour}, nil
			},
			wantStatus:  http.StatusOK,
			wantBody:    `{"access_token":"` + jwtValue + `","token_type":"Bearer","expires_in":3600}`,
			wantNoStore: true,
			wantCalls:   1,
		},
		{
			name: "invalid credentials",
			body: validBody,
			login: func(context.Context, userservice.LoginInput) (userservice.AccessToken, error) {
				return userservice.AccessToken{}, userservice.ErrInvalidCredentials
			},
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"error":{"code":"invalid_credentials","message":"invalid email or password"}}`,
			wantCalls:  1,
		},
		{
			name: "missing fields",
			body: `{"email":"","password":""}`,
			login: func(context.Context, userservice.LoginInput) (userservice.AccessToken, error) {
				return userservice.AccessToken{}, &user.ValidationError{Fields: []user.FieldError{
					{Field: "email", Message: "is required"},
					{Field: "password", Message: "is required"},
				}}
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"email","message":"is required"},{"field":"password","message":"is required"}]}}`,
			wantCalls:  1,
		},
		{
			name: "internal error is hidden from the client and logged",
			body: validBody,
			login: func(context.Context, userservice.LoginInput) (userservice.AccessToken, error) {
				return userservice.AccessToken{}, errors.New("login: mongodb: find user by email: server selection timeout")
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantCalls:  1,
			wantLog:    "server selection timeout",
		},
		{
			name:       "unknown field",
			body:       `{"email":"alice@example.com","password":"correct horse","remember":true}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:       "malformed JSON",
			body:       `{"email":`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{LoginFunc: tt.login}
			var logs bytes.Buffer
			router := NewHandler(service, &AuthenticatorMock{}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, loginPath, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %s\nwant   %s", got, tt.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Header().Get("Cache-Control"); tt.wantNoStore && got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store on a token response", got)
			}

			calls := service.LoginCalls()
			if len(calls) != tt.wantCalls {
				t.Fatalf("Login calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if len(calls) == 1 {
				var want userservice.LoginInput
				if tt.body == validBody {
					want = userservice.LoginInput{Email: "alice@example.com", Password: "correct horse"}
				}
				if calls[0].In != want {
					t.Errorf("Login received %+v, want %+v", calls[0].In, want)
				}
			}

			if tt.wantLog == "" && logs.Len() > 0 {
				t.Errorf("unexpected log output: %s", logs.String())
			}
			if tt.wantLog != "" && !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %s, want it to contain %q", logs.String(), tt.wantLog)
			}
			if strings.Contains(logs.String(), "correct horse") || strings.Contains(logs.String(), jwtValue) {
				t.Errorf("log contains a password or token: %s", logs.String())
			}
		})
	}
}
