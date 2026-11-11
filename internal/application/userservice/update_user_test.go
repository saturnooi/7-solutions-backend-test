package userservice

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"user-management-api/internal/domain/user"
)

func TestService_UpdateUser(t *testing.T) {
	t.Parallel()

	const (
		self  = "6730f1c2a1b2c3d4e5f60718"
		other = "6730f1c2a1b2c3d4e5f60719"
	)
	ptr := func(s string) *string { return &s }
	updated := user.User{
		ID:           self,
		Name:         "Alice Cooper",
		Email:        "alice@example.com",
		PasswordHash: "stored-hash",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 0, 0, 0, time.UTC),
	}
	errDatabase := errors.New("connection reset")
	updateOK := func(context.Context, string, user.Changes) (user.User, error) { return updated, nil }
	failWith := func(err error) func(context.Context, string, user.Changes) (user.User, error) {
		return func(context.Context, string, user.Changes) (user.User, error) {
			return user.User{}, fmt.Errorf("mongodb: update user: %w", err)
		}
	}

	tests := []struct {
		name        string
		callerID    string
		id          string
		input       UpdateInput
		update      func(context.Context, string, user.Changes) (user.User, error)
		want        user.User
		wantErr     error
		wantFields  []user.FieldError
		wantChanges *user.Changes
	}{
		{
			name:        "owner changes their name",
			callerID:    self,
			id:          self,
			input:       UpdateInput{Name: ptr("  Alice Cooper ")},
			update:      updateOK,
			want:        updated,
			wantChanges: &user.Changes{Name: ptr("Alice Cooper")},
		},
		{
			name:        "owner changes their email",
			callerID:    self,
			id:          self,
			input:       UpdateInput{Email: ptr(" ALICE@New.Example.com")},
			update:      updateOK,
			want:        updated,
			wantChanges: &user.Changes{Email: ptr("alice@new.example.com")},
		},
		{
			name:     "another user's account",
			callerID: self,
			id:       other,
			input:    UpdateInput{Name: ptr("Mallory")},
			wantErr:  ErrForbidden,
		},
		{
			name:    "no authenticated caller",
			input:   UpdateInput{Name: ptr("Mallory")},
			wantErr: ErrForbidden,
		},
		{
			name:     "nothing to change",
			callerID: self,
			id:       self,
			wantErr:  user.ErrNoChanges,
		},
		{
			name:       "invalid email is rejected before storing",
			callerID:   self,
			id:         self,
			input:      UpdateInput{Email: ptr("alice")},
			wantFields: []user.FieldError{{Field: "email", Message: "must be a valid email address"}},
		},
		{
			name:        "email belongs to someone else",
			callerID:    self,
			id:          self,
			input:       UpdateInput{Email: ptr("bob@example.com")},
			update:      failWith(user.ErrEmailAlreadyExists),
			wantErr:     user.ErrEmailAlreadyExists,
			wantChanges: &user.Changes{Email: ptr("bob@example.com")},
		},
		{
			name:        "account no longer exists",
			callerID:    self,
			id:          self,
			input:       UpdateInput{Name: ptr("Alice Cooper")},
			update:      failWith(user.ErrNotFound),
			wantErr:     user.ErrNotFound,
			wantChanges: &user.Changes{Name: ptr("Alice Cooper")},
		},
		{
			name:        "repository failure",
			callerID:    self,
			id:          self,
			input:       UpdateInput{Name: ptr("Alice Cooper")},
			update:      failWith(errDatabase),
			wantErr:     errDatabase,
			wantChanges: &user.Changes{Name: ptr("Alice Cooper")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &UserRepositoryMock{UpdateFunc: tt.update}
			service := New(repo, &PasswordHasherMock{}, &TokenIssuerMock{}, time.Now)
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := service.UpdateUser(ctx, tt.callerID, tt.id, tt.input)

			switch {
			case tt.wantFields != nil:
				verr, ok := errors.AsType[*user.ValidationError](err)
				if !ok {
					t.Fatalf("UpdateUser() error = %v, want *user.ValidationError", err)
				}
				if !slices.Equal(verr.Fields, tt.wantFields) {
					t.Errorf("fields = %+v, want %+v", verr.Fields, tt.wantFields)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("UpdateUser() error = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("UpdateUser() error = %v, want nil", err)
				}
			}
			if !sameUser(got, tt.want) {
				t.Errorf("UpdateUser() = %+v, want %+v", got, tt.want)
			}

			calls := repo.UpdateCalls()
			if tt.wantChanges == nil {
				if len(calls) != 0 {
					t.Errorf("Update calls = %d, want 0", len(calls))
				}
				return
			}
			if len(calls) != 1 {
				t.Fatalf("Update calls = %d, want 1", len(calls))
			}
			if calls[0].ID != tt.id {
				t.Errorf("Update received id %q, want %q", calls[0].ID, tt.id)
			}
			if !sameChanges(calls[0].Changes, *tt.wantChanges) {
				t.Errorf("Update received %s, want %s", showChanges(calls[0].Changes), showChanges(*tt.wantChanges))
			}
			if calls[0].Ctx.Value(ctxKey{}) != "request-1" {
				t.Error("Update did not receive the caller's context")
			}
		})
	}
}

func sameChanges(a, b user.Changes) bool {
	same := func(x, y *string) bool {
		if x == nil || y == nil {
			return x == y
		}
		return *x == *y
	}
	return same(a.Name, b.Name) && same(a.Email, b.Email)
}

func showChanges(c user.Changes) string {
	show := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%q", *p)
	}
	return fmt.Sprintf("{Name: %s, Email: %s}", show(c.Name), show(c.Email))
}
