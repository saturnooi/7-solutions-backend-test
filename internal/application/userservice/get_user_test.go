package userservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"user-management-api/internal/domain/user"
)

func TestService_GetUser(t *testing.T) {
	t.Parallel()

	const id = "6730f1c2a1b2c3d4e5f60718"
	stored := user.User{
		ID:           id,
		Name:         "Alice Smith",
		Email:        "alice@example.com",
		PasswordHash: "stored-hash",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 30, 0, 0, time.UTC),
	}
	errDatabase := errors.New("connection reset")

	tests := []struct {
		name    string
		find    func(context.Context, string) (user.User, error)
		want    user.User
		wantErr error
	}{
		{
			name: "existing user",
			find: func(context.Context, string) (user.User, error) { return stored, nil },
			want: stored,
		},
		{
			name: "unknown id",
			find: func(context.Context, string) (user.User, error) {
				return user.User{}, fmt.Errorf("mongodb: find user by id: %w", user.ErrNotFound)
			},
			wantErr: user.ErrNotFound,
		},
		{
			name: "malformed id",
			find: func(context.Context, string) (user.User, error) {
				return user.User{}, fmt.Errorf("mongodb: find user by id: %w", user.ErrInvalidID)
			},
			wantErr: user.ErrInvalidID,
		},
		{
			name:    "repository failure",
			find:    func(context.Context, string) (user.User, error) { return user.User{}, errDatabase },
			wantErr: errDatabase,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &UserRepositoryMock{FindByIDFunc: tt.find}
			service := New(repo, &PasswordHasherMock{}, &TokenIssuerMock{}, time.Now)
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := service.GetUser(ctx, id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetUser() error = %v, want %v", err, tt.wantErr)
			}
			if !sameUser(got, tt.want) {
				t.Errorf("GetUser() = %+v, want %+v", got, tt.want)
			}
			calls := repo.FindByIDCalls()
			if len(calls) != 1 {
				t.Fatalf("FindByID calls = %d, want 1", len(calls))
			}
			if calls[0].ID != id {
				t.Errorf("FindByID received %q, want %q", calls[0].ID, id)
			}
			if calls[0].Ctx.Value(ctxKey{}) != "request-1" {
				t.Error("FindByID did not receive the caller's context")
			}
		})
	}
}
