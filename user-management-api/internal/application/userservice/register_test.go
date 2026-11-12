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

type ctxKey struct{}

func TestService_Register(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 11, 9, 30, 0, 123_456_789, time.UTC)
	input := RegisterInput{Name: "  Alice Smith ", Email: " Alice@Example.com", Password: "correct horse"}
	toStore := user.User{
		Name:         "Alice Smith",
		Email:        "alice@example.com",
		PasswordHash: "hashed-password",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 30, 0, 123_000_000, time.UTC),
	}
	stored := toStore
	stored.ID = "6730f1c2a1b2c3d4e5f60718"

	errHasher := errors.New("hasher unavailable")
	errDatabase := errors.New("connection reset")

	hashOK := func(string) (string, error) { return "hashed-password", nil }
	createOK := func(context.Context, user.User) (user.User, error) { return stored, nil }

	tests := []struct {
		name            string
		input           RegisterInput
		hash            func(string) (string, error)
		create          func(context.Context, user.User) (user.User, error)
		want            user.User
		wantErr         error
		wantFields      []user.FieldError
		wantHashCalls   int
		wantCreateCalls int
	}{
		{
			name:            "stores a normalized user with the hashed password",
			input:           input,
			hash:            hashOK,
			create:          createOK,
			want:            stored,
			wantHashCalls:   1,
			wantCreateCalls: 1,
		},
		{
			name:  "invalid input is rejected before hashing",
			input: RegisterInput{Name: " ", Email: "alice", Password: "short"},
			wantFields: []user.FieldError{
				{Field: "name", Message: "is required"},
				{Field: "email", Message: "must be a valid email address"},
				{Field: "password", Message: "must be at least 8 characters"},
			},
		},
		{
			name:          "hasher failure stops before storing",
			input:         input,
			hash:          func(string) (string, error) { return "", errHasher },
			wantErr:       errHasher,
			wantHashCalls: 1,
		},
		{
			name:  "duplicate email",
			input: input,
			hash:  hashOK,
			create: func(context.Context, user.User) (user.User, error) {
				return user.User{}, fmt.Errorf("mongodb: create user: %w", user.ErrEmailAlreadyExists)
			},
			wantErr:         user.ErrEmailAlreadyExists,
			wantHashCalls:   1,
			wantCreateCalls: 1,
		},
		{
			name:            "repository failure",
			input:           input,
			hash:            hashOK,
			create:          func(context.Context, user.User) (user.User, error) { return user.User{}, errDatabase },
			wantErr:         errDatabase,
			wantHashCalls:   1,
			wantCreateCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hasher := &PasswordHasherMock{HashFunc: tt.hash}
			repo := &UserRepositoryMock{CreateFunc: tt.create}
			service := New(repo, hasher, &TokenIssuerMock{}, func() time.Time { return now })
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := service.Register(ctx, tt.input)

			switch {
			case tt.wantFields != nil:
				verr, ok := errors.AsType[*user.ValidationError](err)
				if !ok {
					t.Fatalf("Register() error = %v, want *user.ValidationError", err)
				}
				if !slices.Equal(verr.Fields, tt.wantFields) {
					t.Errorf("fields = %+v, want %+v", verr.Fields, tt.wantFields)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Register() error = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("Register() error = %v, want nil", err)
				}
			}
			if !sameUser(got, tt.want) {
				t.Errorf("Register() = %+v, want %+v", got, tt.want)
			}

			hashCalls := hasher.HashCalls()
			if len(hashCalls) != tt.wantHashCalls {
				t.Fatalf("Hash calls = %d, want %d", len(hashCalls), tt.wantHashCalls)
			}
			if len(hashCalls) == 1 && hashCalls[0].Password != tt.input.Password {
				t.Errorf("Hash received %q, want the raw password %q", hashCalls[0].Password, tt.input.Password)
			}

			createCalls := repo.CreateCalls()
			if len(createCalls) != tt.wantCreateCalls {
				t.Fatalf("Create calls = %d, want %d", len(createCalls), tt.wantCreateCalls)
			}
			if len(createCalls) == 1 {
				if !sameUser(createCalls[0].U, toStore) {
					t.Errorf("Create received %+v, want %+v", createCalls[0].U, toStore)
				}
				if createCalls[0].Ctx.Value(ctxKey{}) != "request-1" {
					t.Error("Create did not receive the caller's context")
				}
			}
		})
	}
}

func TestService_Register_RejectsEmptyHashFromHasher(t *testing.T) {
	t.Parallel()

	hasher := &PasswordHasherMock{HashFunc: func(string) (string, error) { return "", nil }}
	repo := &UserRepositoryMock{}
	service := New(repo, hasher, &TokenIssuerMock{}, time.Now)

	got, err := service.Register(t.Context(), RegisterInput{Name: "Alice", Email: "alice@example.com", Password: "correct horse"})

	if err == nil {
		t.Fatal("Register() error = nil, want an error for an empty hash")
	}
	if _, ok := errors.AsType[*user.ValidationError](err); ok {
		t.Errorf("Register() error = %v is a *user.ValidationError, want an internal error", err)
	}
	if got != (user.User{}) {
		t.Errorf("Register() = %+v, want the zero User", got)
	}
	if n := len(repo.CreateCalls()); n != 0 {
		t.Errorf("Create calls = %d, want 0", n)
	}
}

func sameUser(a, b user.User) bool {
	return a.ID == b.ID &&
		a.Name == b.Name &&
		a.Email == b.Email &&
		a.PasswordHash == b.PasswordHash &&
		a.CreatedAt.Equal(b.CreatedAt)
}
