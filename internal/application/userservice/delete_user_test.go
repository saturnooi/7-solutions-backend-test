package userservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"user-management-api/internal/domain/user"
)

func TestService_DeleteUser(t *testing.T) {
	t.Parallel()

	const (
		self  = "6730f1c2a1b2c3d4e5f60718"
		other = "6730f1c2a1b2c3d4e5f60719"
	)
	errDatabase := errors.New("connection reset")

	tests := []struct {
		name       string
		callerID   string
		id         string
		delete     func(context.Context, string) error
		wantErr    error
		wantDelete bool
	}{
		{
			name:       "owner deletes their account",
			callerID:   self,
			id:         self,
			delete:     func(context.Context, string) error { return nil },
			wantDelete: true,
		},
		{
			name:     "another user's account",
			callerID: self,
			id:       other,
			wantErr:  ErrForbidden,
		},
		{
			name:    "no authenticated caller",
			wantErr: ErrForbidden,
		},
		{
			name:     "account already gone",
			callerID: self,
			id:       self,
			delete: func(context.Context, string) error {
				return fmt.Errorf("mongodb: delete user: %w", user.ErrNotFound)
			},
			wantErr:    user.ErrNotFound,
			wantDelete: true,
		},
		{
			name:       "repository failure",
			callerID:   self,
			id:         self,
			delete:     func(context.Context, string) error { return errDatabase },
			wantErr:    errDatabase,
			wantDelete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &UserRepositoryMock{DeleteFunc: tt.delete}
			service := New(repo, &PasswordHasherMock{}, &TokenIssuerMock{}, time.Now)
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			err := service.DeleteUser(ctx, tt.callerID, tt.id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DeleteUser() error = %v, want %v", err, tt.wantErr)
			}
			calls := repo.DeleteCalls()
			if !tt.wantDelete {
				if len(calls) != 0 {
					t.Errorf("Delete calls = %d, want 0", len(calls))
				}
				return
			}
			if len(calls) != 1 || calls[0].ID != tt.id {
				t.Fatalf("Delete calls = %+v, want one call with %q", calls, tt.id)
			}
			if calls[0].Ctx.Value(ctxKey{}) != "request-1" {
				t.Error("Delete did not receive the caller's context")
			}
		})
	}
}
