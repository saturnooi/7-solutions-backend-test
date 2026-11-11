package userservice

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"user-management-api/internal/domain/user"
)

func TestService_ListUsers(t *testing.T) {
	t.Parallel()

	stored := []user.User{
		{ID: "6730f1c2a1b2c3d4e5f60719", Name: "Bob Jones", Email: "bob@example.com", PasswordHash: "hash-b", CreatedAt: time.Date(2026, time.November, 11, 10, 0, 0, 0, time.UTC)},
		{ID: "6730f1c2a1b2c3d4e5f60718", Name: "Alice Smith", Email: "alice@example.com", PasswordHash: "hash-a", CreatedAt: time.Date(2026, time.November, 11, 9, 0, 0, 0, time.UTC)},
	}
	errDatabase := errors.New("connection reset")
	listStored := func(context.Context, int, int) ([]user.User, error) { return stored, nil }

	tests := []struct {
		name       string
		input      ListInput
		list       func(context.Context, int, int) ([]user.User, error)
		want       UserPage
		wantErr    error
		wantFields []user.FieldError
		wantCalls  int
	}{
		{
			name:      "first page",
			input:     ListInput{Limit: 20, Offset: 0},
			list:      listStored,
			want:      UserPage{Users: stored, Limit: 20, Offset: 0},
			wantCalls: 1,
		},
		{
			name:      "smallest and largest allowed values",
			input:     ListInput{Limit: 1, Offset: 1_000_000},
			list:      func(context.Context, int, int) ([]user.User, error) { return []user.User{}, nil },
			want:      UserPage{Users: []user.User{}, Limit: 1, Offset: 1_000_000},
			wantCalls: 1,
		},
		{
			name:      "maximum page size",
			input:     ListInput{Limit: 100, Offset: 0},
			list:      listStored,
			want:      UserPage{Users: stored, Limit: 100, Offset: 0},
			wantCalls: 1,
		},
		{
			name:       "limit of zero",
			input:      ListInput{Limit: 0, Offset: 0},
			wantFields: []user.FieldError{{Field: "limit", Message: "must be between 1 and 100"}},
		},
		{
			name:       "limit above the maximum",
			input:      ListInput{Limit: 101, Offset: 0},
			wantFields: []user.FieldError{{Field: "limit", Message: "must be between 1 and 100"}},
		},
		{
			name:       "negative offset",
			input:      ListInput{Limit: 20, Offset: -1},
			wantFields: []user.FieldError{{Field: "offset", Message: "must not be negative"}},
		},
		{
			name:  "both invalid",
			input: ListInput{Limit: -5, Offset: -5},
			wantFields: []user.FieldError{
				{Field: "limit", Message: "must be between 1 and 100"},
				{Field: "offset", Message: "must not be negative"},
			},
		},
		{
			name:      "repository failure",
			input:     ListInput{Limit: 20, Offset: 0},
			list:      func(context.Context, int, int) ([]user.User, error) { return nil, errDatabase },
			wantErr:   errDatabase,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &UserRepositoryMock{ListFunc: tt.list}
			service := New(repo, &PasswordHasherMock{}, &TokenIssuerMock{}, time.Now)
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := service.ListUsers(ctx, tt.input)

			switch {
			case tt.wantFields != nil:
				verr, ok := errors.AsType[*user.ValidationError](err)
				if !ok {
					t.Fatalf("ListUsers() error = %v, want *user.ValidationError", err)
				}
				if !slices.Equal(verr.Fields, tt.wantFields) {
					t.Errorf("fields = %+v, want %+v", verr.Fields, tt.wantFields)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ListUsers() error = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("ListUsers() error = %v, want nil", err)
				}
			}
			if got.Limit != tt.want.Limit || got.Offset != tt.want.Offset || !slices.EqualFunc(got.Users, tt.want.Users, sameUser) {
				t.Errorf("ListUsers() = %+v, want %+v", got, tt.want)
			}

			calls := repo.ListCalls()
			if len(calls) != tt.wantCalls {
				t.Fatalf("List calls = %d, want %d", len(calls), tt.wantCalls)
			}
			if len(calls) == 1 {
				if calls[0].Limit != tt.input.Limit || calls[0].Offset != tt.input.Offset {
					t.Errorf("List received limit %d offset %d, want %d and %d", calls[0].Limit, calls[0].Offset, tt.input.Limit, tt.input.Offset)
				}
				if calls[0].Ctx.Value(ctxKey{}) != "request-1" {
					t.Error("List did not receive the caller's context")
				}
			}
		})
	}
}
