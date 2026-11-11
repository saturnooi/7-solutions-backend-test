package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"user-management-api/internal/adapters/auth"
	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

const (
	usersPath     = "/api/v1/users"
	createBody    = `{"name":"Bob Jones","email":"bob@example.com","password":"correct horse"}`
	createdBody   = `{"id":"6730f1c2a1b2c3d4e5f60719","name":"Bob Jones","email":"bob@example.com","created_at":"2026-11-11T10:00:00Z"}`
	testJWTSecret = "test-secret-that-is-at-least-32-bytes-long"
	testJWTIssuer = "user-management-api"
	callerID      = "6730f1c2a1b2c3d4e5f60718"
)

var created = user.User{
	ID:           "6730f1c2a1b2c3d4e5f60719",
	Name:         "Bob Jones",
	Email:        "bob@example.com",
	PasswordHash: "$2a$12$stored-hash-value",
	CreatedAt:    time.Date(2026, time.November, 11, 10, 0, 0, 0, time.UTC),
}

func TestCreateUser(t *testing.T) {
	t.Parallel()

	accept := func(string) (string, error) { return callerID, nil }

	tests := []struct {
		name          string
		authorization string
		verify        func(string) (string, error)
		register      func(context.Context, userservice.RegisterInput) (user.User, error)
		wantStatus    int
		wantBody      string
		wantCalls     int
	}{
		{
			name:          "authenticated caller creates a user",
			authorization: "Bearer valid.token.signature",
			verify:        accept,
			register:      func(context.Context, userservice.RegisterInput) (user.User, error) { return created, nil },
			wantStatus:    http.StatusCreated,
			wantBody:      createdBody,
			wantCalls:     1,
		},
		{
			name:          "validation still applies",
			authorization: "Bearer valid.token.signature",
			verify:        accept,
			register: func(context.Context, userservice.RegisterInput) (user.User, error) {
				return user.User{}, &user.ValidationError{Fields: []user.FieldError{{Field: "password", Message: "must be at least 8 characters"}}}
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"password","message":"must be at least 8 characters"}]}}`,
			wantCalls:  1,
		},
		{
			name:       "no token",
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthorizedBody,
		},
		{
			name:          "rejected token",
			authorization: "Bearer forged.token.signature",
			verify:        func(string) (string, error) { return "", errors.New("jwt: invalid token") },
			wantStatus:    http.StatusUnauthorized,
			wantBody:      unauthorizedBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{RegisterFunc: tt.register}
			router := NewHandler(service, &TokenVerifierMock{VerifyFunc: tt.verify}, slog.New(slog.DiscardHandler)).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, usersPath, strings.NewReader(createBody))
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %s\nwant   %s", got, tt.wantBody)
			}
			calls := service.RegisterCalls()
			if len(calls) != tt.wantCalls {
				t.Fatalf("Register calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if len(calls) == 1 {
				want := userservice.RegisterInput{Name: "Bob Jones", Email: "bob@example.com", Password: "correct horse"}
				if calls[0].In != want {
					t.Errorf("Register received %+v, want %+v", calls[0].In, want)
				}
			}
		})
	}
}

func TestCreateUser_WithRealJWT(t *testing.T) {
	t.Parallel()

	now := time.Now()
	verifier := newJWTService(t, testJWTSecret, func() time.Time { return now })

	tests := []struct {
		name       string
		issuer     *auth.JWTService
		wantStatus int
	}{
		{name: "token from login", issuer: verifier, wantStatus: http.StatusCreated},
		{name: "expired token", issuer: newJWTService(t, testJWTSecret, func() time.Time { return now.Add(-2 * time.Hour) }), wantStatus: http.StatusUnauthorized},
		{name: "token signed with another secret", issuer: newJWTService(t, "another-secret-that-is-at-least-32-bytes", func() time.Time { return now }), wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token, err := tt.issuer.Issue(callerID)
			if err != nil {
				t.Fatalf("Issue() error = %v", err)
			}
			service := &UserServiceMock{RegisterFunc: func(context.Context, userservice.RegisterInput) (user.User, error) { return created, nil }}
			router := NewHandler(service, verifier, slog.New(slog.DiscardHandler)).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, usersPath, strings.NewReader(createBody))
			req.Header.Set("Authorization", "Bearer "+token.Value)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func newJWTService(t *testing.T, secret string, now func() time.Time) *auth.JWTService {
	t.Helper()

	service, err := auth.NewJWTService([]byte(secret), testJWTIssuer, time.Hour, now)
	if err != nil {
		t.Fatalf("NewJWTService() error = %v", err)
	}
	return service
}
