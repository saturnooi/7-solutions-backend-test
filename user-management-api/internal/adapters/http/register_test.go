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
	"time"

	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

const registerPath = "/api/v1/auth/register"

func TestRegister(t *testing.T) {
	t.Parallel()

	const validBody = `{"name":"Alice Smith","email":"alice@example.com","password":"correct horse"}`

	registered := user.User{
		ID:           "6730f1c2a1b2c3d4e5f60718",
		Name:         "Alice Smith",
		Email:        "alice@example.com",
		PasswordHash: "$2a$12$stored-hash-value",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 30, 0, 123_000_000, time.UTC),
	}

	tests := []struct {
		name       string
		body       string
		register   func(context.Context, userservice.RegisterInput) (user.User, error)
		wantStatus int
		wantBody   string
		wantCalls  int
		wantLog    string
	}{
		{
			name:       "created",
			body:       validBody + "\n",
			register:   func(context.Context, userservice.RegisterInput) (user.User, error) { return registered, nil },
			wantStatus: http.StatusCreated,
			wantBody:   `{"id":"6730f1c2a1b2c3d4e5f60718","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-11T09:30:00.123Z"}`,
			wantCalls:  1,
		},
		{
			name: "validation failed",
			body: validBody,
			register: func(context.Context, userservice.RegisterInput) (user.User, error) {
				return user.User{}, &user.ValidationError{Fields: []user.FieldError{
					{Field: "email", Message: "must be a valid email address"},
					{Field: "password", Message: "must be at least 8 characters"},
				}}
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"email","message":"must be a valid email address"},{"field":"password","message":"must be at least 8 characters"}]}}`,
			wantCalls:  1,
		},
		{
			name: "email already registered",
			body: validBody,
			register: func(context.Context, userservice.RegisterInput) (user.User, error) {
				return user.User{}, fmt.Errorf("register user: %w", user.ErrEmailAlreadyExists)
			},
			wantStatus: http.StatusConflict,
			wantBody:   `{"error":{"code":"email_already_exists","message":"email is already registered"}}`,
			wantCalls:  1,
		},
		{
			name: "internal error is hidden from the client and logged",
			body: validBody,
			register: func(context.Context, userservice.RegisterInput) (user.User, error) {
				return user.User{}, errors.New("register user: mongodb: create user: connection refused")
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantCalls:  1,
			wantLog:    "connection refused",
		},
		{
			name:       "malformed JSON",
			body:       `{"name":`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:       "empty body",
			body:       "",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:       "unknown field",
			body:       `{"name":"Alice","email":"alice@example.com","password":"correct horse","role":"admin"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:       "wrong field type",
			body:       `{"name":42,"email":"alice@example.com","password":"correct horse"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:       "two JSON objects",
			body:       validBody + validBody,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:       "valid object padded past 1 MiB",
			body:       validBody + strings.Repeat(" ", maxBodyBytes),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantBody:   `{"error":{"code":"request_too_large","message":"request body must not exceed 1 MiB"}}`,
		},
		{
			name:       "body over 1 MiB",
			body:       `{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantBody:   `{"error":{"code":"request_too_large","message":"request body must not exceed 1 MiB"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{RegisterFunc: tt.register}
			var logs bytes.Buffer
			router := NewHandler(service, &AuthenticatorMock{}, &HealthCheckerMock{}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, registerPath, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %s\nwant   %s", got, tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), "password_hash") || strings.Contains(rec.Body.String(), "$2a$") {
				t.Errorf("body exposes the password hash: %s", rec.Body.String())
			}

			calls := service.RegisterCalls()
			if len(calls) != tt.wantCalls {
				t.Fatalf("Register calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if len(calls) == 1 {
				want := userservice.RegisterInput{Name: "Alice Smith", Email: "alice@example.com", Password: "correct horse"}
				if calls[0].In != want {
					t.Errorf("Register received %+v, want %+v", calls[0].In, want)
				}
			}

			if tt.wantLog == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
			}
			if tt.wantLog != "" && !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %s, want it to contain %q", logs.String(), tt.wantLog)
			}
			if strings.Contains(logs.String(), "correct horse") {
				t.Errorf("log contains the password: %s", logs.String())
			}
		})
	}
}

func TestRegister_OnlyAcceptsPost(t *testing.T) {
	t.Parallel()

	router := NewHandler(&UserServiceMock{}, &AuthenticatorMock{}, &HealthCheckerMock{}, slog.New(slog.DiscardHandler)).Routes()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, registerPath, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want %q", got, http.MethodPost)
	}
}
