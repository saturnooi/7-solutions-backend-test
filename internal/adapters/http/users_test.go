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

func TestGetUser(t *testing.T) {
	t.Parallel()

	accept := func(string) (string, error) { return callerID, nil }

	tests := []struct {
		name          string
		path          string
		authorization string
		getUser       func(context.Context, string) (user.User, error)
		wantStatus    int
		wantBody      string
		wantID        string
		wantLog       string
	}{
		{
			name:          "existing user",
			path:          usersPath + "/" + created.ID,
			authorization: "Bearer valid.token.signature",
			getUser:       func(context.Context, string) (user.User, error) { return created, nil },
			wantStatus:    http.StatusOK,
			wantBody:      createdBody,
			wantID:        created.ID,
		},
		{
			name:          "unknown id",
			path:          usersPath + "/6730f1c2a1b2c3d4e5f60000",
			authorization: "Bearer valid.token.signature",
			getUser: func(context.Context, string) (user.User, error) {
				return user.User{}, fmt.Errorf("get user: %w", user.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"user_not_found","message":"user not found"}}`,
			wantID:     "6730f1c2a1b2c3d4e5f60000",
		},
		{
			name:          "malformed id",
			path:          usersPath + "/not-an-id",
			authorization: "Bearer valid.token.signature",
			getUser: func(context.Context, string) (user.User, error) {
				return user.User{}, fmt.Errorf("get user: %w", user.ErrInvalidID)
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"invalid_id","message":"id must be a 24-character hexadecimal string"}}`,
			wantID:     "not-an-id",
		},
		{
			name:          "internal error is hidden from the client and logged",
			path:          usersPath + "/" + created.ID,
			authorization: "Bearer valid.token.signature",
			getUser: func(context.Context, string) (user.User, error) {
				return user.User{}, errors.New("get user: mongodb: find user by id: connection reset")
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantID:     created.ID,
			wantLog:    "connection reset",
		},
		{
			name:       "no token",
			path:       usersPath + "/" + created.ID,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthorizedBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{GetUserFunc: tt.getUser}
			var logs bytes.Buffer
			router := NewHandler(service, &TokenVerifierMock{VerifyFunc: accept}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
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
			calls := service.GetUserCalls()
			switch {
			case tt.wantID == "" && len(calls) != 0:
				t.Errorf("GetUser calls = %d, want 0", len(calls))
			case tt.wantID != "" && (len(calls) != 1 || calls[0].ID != tt.wantID):
				t.Errorf("GetUser calls = %+v, want one call with %q", calls, tt.wantID)
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

func TestListUsers(t *testing.T) {
	t.Parallel()

	alice := user.User{
		ID:           callerID,
		Name:         "Alice Smith",
		Email:        "alice@example.com",
		PasswordHash: "$2a$12$another-stored-hash",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 0, 0, 0, time.UTC),
	}
	accept := func(string) (string, error) { return callerID, nil }
	pageOf := func(users ...user.User) func(context.Context, userservice.ListInput) (userservice.UserPage, error) {
		return func(_ context.Context, in userservice.ListInput) (userservice.UserPage, error) {
			return userservice.UserPage{Users: users, Limit: in.Limit, Offset: in.Offset}, nil
		}
	}

	tests := []struct {
		name          string
		query         string
		authorization string
		list          func(context.Context, userservice.ListInput) (userservice.UserPage, error)
		wantStatus    int
		wantBody      string
		wantInput     *userservice.ListInput
		wantLog       string
	}{
		{
			name:          "defaults when no query is given",
			authorization: "Bearer valid.token.signature",
			list:          pageOf(created, alice),
			wantStatus:    http.StatusOK,
			wantBody: `{"users":[` +
				`{"id":"6730f1c2a1b2c3d4e5f60719","name":"Bob Jones","email":"bob@example.com","created_at":"2026-11-11T10:00:00Z"},` +
				`{"id":"6730f1c2a1b2c3d4e5f60718","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-11T09:00:00Z"}` +
				`],"limit":20,"offset":0}`,
			wantInput: &userservice.ListInput{Limit: 20, Offset: 0},
		},
		{
			name:          "explicit limit and offset",
			query:         "?limit=1&offset=1",
			authorization: "Bearer valid.token.signature",
			list:          pageOf(alice),
			wantStatus:    http.StatusOK,
			wantBody:      `{"users":[{"id":"6730f1c2a1b2c3d4e5f60718","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-11T09:00:00Z"}],"limit":1,"offset":1}`,
			wantInput:     &userservice.ListInput{Limit: 1, Offset: 1},
		},
		{
			name:          "empty page is an empty array",
			query:         "?offset=500",
			authorization: "Bearer valid.token.signature",
			list:          pageOf(),
			wantStatus:    http.StatusOK,
			wantBody:      `{"users":[],"limit":20,"offset":500}`,
			wantInput:     &userservice.ListInput{Limit: 20, Offset: 500},
		},
		{
			name:          "empty parameters fall back to defaults",
			query:         "?limit=&offset=",
			authorization: "Bearer valid.token.signature",
			list:          pageOf(),
			wantStatus:    http.StatusOK,
			wantBody:      `{"users":[],"limit":20,"offset":0}`,
			wantInput:     &userservice.ListInput{Limit: 20, Offset: 0},
		},
		{
			name:          "non-numeric parameters",
			query:         "?limit=ten&offset=1.5",
			authorization: "Bearer valid.token.signature",
			wantStatus:    http.StatusBadRequest,
			wantBody:      `{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"limit","message":"must be an integer"},{"field":"offset","message":"must be an integer"}]}}`,
		},
		{
			name:          "out-of-range values are rejected by the use case",
			query:         "?limit=1000",
			authorization: "Bearer valid.token.signature",
			list: func(context.Context, userservice.ListInput) (userservice.UserPage, error) {
				return userservice.UserPage{}, &user.ValidationError{Fields: []user.FieldError{{Field: "limit", Message: "must be between 1 and 100"}}}
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"limit","message":"must be between 1 and 100"}]}}`,
			wantInput:  &userservice.ListInput{Limit: 1000, Offset: 0},
		},
		{
			name:          "internal error is hidden from the client and logged",
			authorization: "Bearer valid.token.signature",
			list: func(context.Context, userservice.ListInput) (userservice.UserPage, error) {
				return userservice.UserPage{}, errors.New("list users: mongodb: list users: cursor killed")
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantInput:  &userservice.ListInput{Limit: 20, Offset: 0},
			wantLog:    "cursor killed",
		},
		{
			name:       "no token",
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthorizedBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{ListUsersFunc: tt.list}
			var logs bytes.Buffer
			router := NewHandler(service, &TokenVerifierMock{VerifyFunc: accept}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, usersPath+tt.query, nil)
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
			calls := service.ListUsersCalls()
			switch {
			case tt.wantInput == nil && len(calls) != 0:
				t.Errorf("ListUsers calls = %d, want 0", len(calls))
			case tt.wantInput != nil && (len(calls) != 1 || calls[0].In != *tt.wantInput):
				t.Errorf("ListUsers calls = %+v, want one call with %+v", calls, *tt.wantInput)
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
