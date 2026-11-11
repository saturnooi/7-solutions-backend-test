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

	accept := func(context.Context, string) (string, error) { return callerID, nil }

	tests := []struct {
		name          string
		authorization string
		authenticate  func(context.Context, string) (string, error)
		register      func(context.Context, userservice.RegisterInput) (user.User, error)
		wantStatus    int
		wantBody      string
		wantCalls     int
	}{
		{
			name:          "authenticated caller creates a user",
			authorization: "Bearer valid.token.signature",
			authenticate:  accept,
			register:      func(context.Context, userservice.RegisterInput) (user.User, error) { return created, nil },
			wantStatus:    http.StatusCreated,
			wantBody:      createdBody,
			wantCalls:     1,
		},
		{
			name:          "validation still applies",
			authorization: "Bearer valid.token.signature",
			authenticate:  accept,
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
			authenticate: func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w: jwt: invalid token", userservice.ErrUnauthenticated)
			},
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthorizedBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{RegisterFunc: tt.register}
			router := NewHandler(service, &AuthenticatorMock{AuthenticateFunc: tt.authenticate}, slog.New(slog.DiscardHandler)).Routes()
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

	accept := func(context.Context, string) (string, error) { return callerID, nil }

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
			router := NewHandler(service, &AuthenticatorMock{AuthenticateFunc: accept}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
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
			if tt.wantLog == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
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
	accept := func(context.Context, string) (string, error) { return callerID, nil }
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
			router := NewHandler(service, &AuthenticatorMock{AuthenticateFunc: accept}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
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
			if tt.wantLog == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
			}
			if tt.wantLog != "" && !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %s, want it to contain %q", logs.String(), tt.wantLog)
			}
		})
	}
}

func TestUpdateUser(t *testing.T) {
	t.Parallel()

	const updatedBody = `{"id":"6730f1c2a1b2c3d4e5f60718","name":"Alice Cooper","email":"alice@example.com","created_at":"2026-11-11T09:00:00Z"}`
	ptr := func(s string) *string { return &s }
	updated := user.User{
		ID:           callerID,
		Name:         "Alice Cooper",
		Email:        "alice@example.com",
		PasswordHash: "$2a$12$stored-hash-value",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 0, 0, 0, time.UTC),
	}
	accept := func(context.Context, string) (string, error) { return callerID, nil }
	returns := func(u user.User, err error) func(context.Context, string, string, userservice.UpdateInput) (user.User, error) {
		return func(context.Context, string, string, userservice.UpdateInput) (user.User, error) { return u, err }
	}

	tests := []struct {
		name          string
		target        string
		body          string
		authorization string
		update        func(context.Context, string, string, userservice.UpdateInput) (user.User, error)
		wantStatus    int
		wantBody      string
		wantInput     *userservice.UpdateInput
		wantLog       string
	}{
		{
			name:          "owner changes their name",
			target:        callerID,
			body:          `{"name":"Alice Cooper"}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(updated, nil),
			wantStatus:    http.StatusOK,
			wantBody:      updatedBody,
			wantInput:     &userservice.UpdateInput{Name: ptr("Alice Cooper")},
		},
		{
			name:          "null leaves a field unchanged",
			target:        callerID,
			body:          `{"name":null,"email":"alice@example.com"}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(updated, nil),
			wantStatus:    http.StatusOK,
			wantBody:      updatedBody,
			wantInput:     &userservice.UpdateInput{Email: ptr("alice@example.com")},
		},
		{
			name:          "another user's account",
			target:        created.ID,
			body:          `{"name":"Mallory"}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(user.User{}, userservice.ErrForbidden),
			wantStatus:    http.StatusForbidden,
			wantBody:      `{"error":{"code":"forbidden","message":"you may only change your own account"}}`,
			wantInput:     &userservice.UpdateInput{Name: ptr("Mallory")},
		},
		{
			name:          "nothing to change",
			target:        callerID,
			body:          `{}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(user.User{}, user.ErrNoChanges),
			wantStatus:    http.StatusBadRequest,
			wantBody:      `{"error":{"code":"empty_update","message":"request must change name or email"}}`,
			wantInput:     &userservice.UpdateInput{},
		},
		{
			name:          "invalid field",
			target:        callerID,
			body:          `{"email":"alice"}`,
			authorization: "Bearer valid.token.signature",
			update: returns(user.User{}, &user.ValidationError{Fields: []user.FieldError{
				{Field: "email", Message: "must be a valid email address"},
			}}),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"email","message":"must be a valid email address"}]}}`,
			wantInput:  &userservice.UpdateInput{Email: ptr("alice")},
		},
		{
			name:          "email belongs to someone else",
			target:        callerID,
			body:          `{"email":"bob@example.com"}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(user.User{}, fmt.Errorf("update user: %w", user.ErrEmailAlreadyExists)),
			wantStatus:    http.StatusConflict,
			wantBody:      `{"error":{"code":"email_already_exists","message":"email is already registered"}}`,
			wantInput:     &userservice.UpdateInput{Email: ptr("bob@example.com")},
		},
		{
			name:          "account no longer exists",
			target:        callerID,
			body:          `{"name":"Alice Cooper"}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(user.User{}, fmt.Errorf("update user: %w", user.ErrNotFound)),
			wantStatus:    http.StatusNotFound,
			wantBody:      `{"error":{"code":"user_not_found","message":"user not found"}}`,
			wantInput:     &userservice.UpdateInput{Name: ptr("Alice Cooper")},
		},
		{
			name:          "password cannot be changed here",
			target:        callerID,
			body:          `{"password":"a new password"}`,
			authorization: "Bearer valid.token.signature",
			wantStatus:    http.StatusBadRequest,
			wantBody:      `{"error":{"code":"invalid_json","message":"request body must be a single JSON object with only the documented fields"}}`,
		},
		{
			name:          "internal error is hidden from the client and logged",
			target:        callerID,
			body:          `{"name":"Alice Cooper"}`,
			authorization: "Bearer valid.token.signature",
			update:        returns(user.User{}, errors.New("update user: mongodb: update user: write concern timeout")),
			wantStatus:    http.StatusInternalServerError,
			wantBody:      `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantInput:     &userservice.UpdateInput{Name: ptr("Alice Cooper")},
			wantLog:       "write concern timeout",
		},
		{
			name:       "no token",
			target:     callerID,
			body:       `{"name":"Alice Cooper"}`,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthorizedBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{UpdateUserFunc: tt.update}
			var logs bytes.Buffer
			router := NewHandler(service, &AuthenticatorMock{AuthenticateFunc: accept}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, usersPath+"/"+tt.target, strings.NewReader(tt.body))
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

			calls := service.UpdateUserCalls()
			if tt.wantInput == nil {
				if len(calls) != 0 {
					t.Errorf("UpdateUser calls = %d, want 0", len(calls))
				}
			} else {
				if len(calls) != 1 {
					t.Fatalf("UpdateUser calls = %d, want 1", len(calls))
				}
				if calls[0].CallerID != callerID || calls[0].ID != tt.target {
					t.Errorf("UpdateUser received caller %q and id %q, want %q and %q", calls[0].CallerID, calls[0].ID, callerID, tt.target)
				}
				if !samePointee(calls[0].In.Name, tt.wantInput.Name) || !samePointee(calls[0].In.Email, tt.wantInput.Email) {
					t.Errorf("UpdateUser received %s, want %s", showInput(calls[0].In), showInput(*tt.wantInput))
				}
			}

			if tt.wantLog == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
			}
			if tt.wantLog != "" && !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %s, want it to contain %q", logs.String(), tt.wantLog)
			}
		})
	}
}

func samePointee(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func showInput(in userservice.UpdateInput) string {
	show := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%q", *p)
	}
	return fmt.Sprintf("{Name: %s, Email: %s}", show(in.Name), show(in.Email))
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()

	accept := func(context.Context, string) (string, error) { return callerID, nil }
	returns := func(err error) func(context.Context, string, string) error {
		return func(context.Context, string, string) error { return err }
	}

	tests := []struct {
		name          string
		target        string
		authorization string
		deleteUser    func(context.Context, string, string) error
		wantStatus    int
		wantBody      string
		wantCall      bool
		wantLog       string
	}{
		{
			name:          "owner deletes their account",
			target:        callerID,
			authorization: "Bearer valid.token.signature",
			deleteUser:    returns(nil),
			wantStatus:    http.StatusNoContent,
			wantCall:      true,
		},
		{
			name:          "another user's account",
			target:        created.ID,
			authorization: "Bearer valid.token.signature",
			deleteUser:    returns(userservice.ErrForbidden),
			wantStatus:    http.StatusForbidden,
			wantBody:      `{"error":{"code":"forbidden","message":"you may only change your own account"}}`,
			wantCall:      true,
		},
		{
			name:          "account already gone",
			target:        callerID,
			authorization: "Bearer valid.token.signature",
			deleteUser:    returns(fmt.Errorf("delete user: %w", user.ErrNotFound)),
			wantStatus:    http.StatusNotFound,
			wantBody:      `{"error":{"code":"user_not_found","message":"user not found"}}`,
			wantCall:      true,
		},
		{
			name:          "internal error is hidden from the client and logged",
			target:        callerID,
			authorization: "Bearer valid.token.signature",
			deleteUser:    returns(errors.New("delete user: mongodb: delete user: not primary")),
			wantStatus:    http.StatusInternalServerError,
			wantBody:      `{"error":{"code":"internal_error","message":"internal server error"}}`,
			wantCall:      true,
			wantLog:       "not primary",
		},
		{
			name:       "no token",
			target:     callerID,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthorizedBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{DeleteUserFunc: tt.deleteUser}
			var logs bytes.Buffer
			router := NewHandler(service, &AuthenticatorMock{AuthenticateFunc: accept}, slog.New(slog.NewJSONHandler(&logs, nil))).Routes()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, usersPath+"/"+tt.target, nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			calls := service.DeleteUserCalls()
			switch {
			case !tt.wantCall && len(calls) != 0:
				t.Errorf("DeleteUser calls = %d, want 0", len(calls))
			case tt.wantCall && (len(calls) != 1 || calls[0].CallerID != callerID || calls[0].ID != tt.target):
				t.Errorf("DeleteUser calls = %+v, want one call with caller %q and id %q", calls, callerID, tt.target)
			}
			if tt.wantLog == "" && errorLogs(&logs) != "" {
				t.Errorf("unexpected error log: %s", errorLogs(&logs))
			}
			if tt.wantLog != "" && !strings.Contains(logs.String(), tt.wantLog) {
				t.Errorf("log = %s, want it to contain %q", logs.String(), tt.wantLog)
			}
		})
	}
}

type finderFunc func(ctx context.Context, id string) (user.User, error)

func (f finderFunc) FindByID(ctx context.Context, id string) (user.User, error) { return f(ctx, id) }

func TestProtectedRoute_WithRealAuthentication(t *testing.T) {
	t.Parallel()

	now := time.Now()
	verifier := newJWTService(t, testJWTSecret, func() time.Time { return now })
	exists := finderFunc(func(context.Context, string) (user.User, error) { return created, nil })
	deleted := finderFunc(func(context.Context, string) (user.User, error) {
		return user.User{}, fmt.Errorf("mongodb: find user by id: %w", user.ErrNotFound)
	})

	tests := []struct {
		name       string
		issuer     *auth.JWTService
		users      finderFunc
		wantStatus int
	}{
		{name: "token from login for an existing user", issuer: verifier, users: exists, wantStatus: http.StatusCreated},
		{name: "valid token whose user was deleted", issuer: verifier, users: deleted, wantStatus: http.StatusUnauthorized},
		{name: "expired token", issuer: newJWTService(t, testJWTSecret, func() time.Time { return now.Add(-2 * time.Hour) }), users: exists, wantStatus: http.StatusUnauthorized},
		{name: "token signed with another secret", issuer: newJWTService(t, "another-secret-that-is-at-least-32-bytes", func() time.Time { return now }), users: exists, wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token, err := tt.issuer.Issue(callerID)
			if err != nil {
				t.Fatalf("Issue() error = %v", err)
			}
			service := &UserServiceMock{RegisterFunc: func(context.Context, userservice.RegisterInput) (user.User, error) { return created, nil }}
			router := NewHandler(service, userservice.NewAuthenticator(verifier, tt.users), slog.New(slog.DiscardHandler)).Routes()
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
