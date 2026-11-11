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

func TestService_Login(t *testing.T) {
	t.Parallel()

	stored := user.User{
		ID:           "6730f1c2a1b2c3d4e5f60718",
		Name:         "Alice Smith",
		Email:        "alice@example.com",
		PasswordHash: "stored-hash",
		CreatedAt:    time.Date(2026, time.November, 11, 9, 30, 0, 0, time.UTC),
	}
	token := AccessToken{Value: "header.payload.signature", ExpiresIn: time.Hour}
	input := LoginInput{Email: "  Alice@Example.com ", Password: "correct horse"}

	errDatabase := errors.New("connection reset")
	errHasher := errors.New("hasher unavailable")
	errSigner := errors.New("signer unavailable")

	findStored := func(context.Context, string) (user.User, error) { return stored, nil }
	findNone := func(context.Context, string) (user.User, error) {
		return user.User{}, fmt.Errorf("mongodb: find user by email: %w", user.ErrNotFound)
	}
	hashDummy := func(string) (string, error) { return "dummy-hash", nil }
	match := func(string, string) (bool, error) { return true, nil }
	mismatch := func(string, string) (bool, error) { return false, nil }
	issueOK := func(string) (AccessToken, error) { return token, nil }

	tests := []struct {
		name           string
		input          LoginInput
		find           func(context.Context, string) (user.User, error)
		hash           func(string) (string, error)
		compare        func(string, string) (bool, error)
		issue          func(string) (AccessToken, error)
		want           AccessToken
		wantErr        error
		wantFields     []user.FieldError
		wantFindCalls  int
		wantComparedTo string
		wantIssueCalls int
	}{
		{
			name:           "valid credentials return a token",
			input:          input,
			find:           findStored,
			compare:        match,
			issue:          issueOK,
			want:           token,
			wantFindCalls:  1,
			wantComparedTo: "stored-hash",
			wantIssueCalls: 1,
		},
		{
			name:  "missing email and password",
			input: LoginInput{Email: "  ", Password: ""},
			wantFields: []user.FieldError{
				{Field: "email", Message: "is required"},
				{Field: "password", Message: "is required"},
			},
		},
		{
			name:           "wrong password",
			input:          input,
			find:           findStored,
			compare:        mismatch,
			wantErr:        ErrInvalidCredentials,
			wantFindCalls:  1,
			wantComparedTo: "stored-hash",
		},
		{
			name:           "unknown email still runs a password comparison",
			input:          input,
			find:           findNone,
			hash:           hashDummy,
			compare:        mismatch,
			wantErr:        ErrInvalidCredentials,
			wantFindCalls:  1,
			wantComparedTo: "dummy-hash",
		},
		{
			name:          "repository failure is not reported as bad credentials",
			input:         input,
			find:          func(context.Context, string) (user.User, error) { return user.User{}, errDatabase },
			wantErr:       errDatabase,
			wantFindCalls: 1,
		},
		{
			name:          "dummy hash failure",
			input:         input,
			find:          findNone,
			hash:          func(string) (string, error) { return "", errHasher },
			wantErr:       errHasher,
			wantFindCalls: 1,
		},
		{
			name:           "dummy comparison failure",
			input:          input,
			find:           findNone,
			hash:           hashDummy,
			compare:        func(string, string) (bool, error) { return false, errHasher },
			wantErr:        errHasher,
			wantFindCalls:  1,
			wantComparedTo: "dummy-hash",
		},
		{
			name:           "comparison failure",
			input:          input,
			find:           findStored,
			compare:        func(string, string) (bool, error) { return false, errHasher },
			wantErr:        errHasher,
			wantFindCalls:  1,
			wantComparedTo: "stored-hash",
		},
		{
			name:           "token signing failure",
			input:          input,
			find:           findStored,
			compare:        match,
			issue:          func(string) (AccessToken, error) { return AccessToken{}, errSigner },
			wantErr:        errSigner,
			wantFindCalls:  1,
			wantComparedTo: "stored-hash",
			wantIssueCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &UserRepositoryMock{FindByEmailFunc: tt.find}
			hasher := &PasswordHasherMock{HashFunc: tt.hash, CompareFunc: tt.compare}
			tokens := &TokenIssuerMock{IssueFunc: tt.issue}
			service := New(repo, hasher, tokens, time.Now)
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := service.Login(ctx, tt.input)

			switch {
			case tt.wantFields != nil:
				verr, ok := errors.AsType[*user.ValidationError](err)
				if !ok {
					t.Fatalf("Login() error = %v, want *user.ValidationError", err)
				}
				if !slices.Equal(verr.Fields, tt.wantFields) {
					t.Errorf("fields = %+v, want %+v", verr.Fields, tt.wantFields)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Login() error = %v, want %v", err, tt.wantErr)
				}
				if !errors.Is(tt.wantErr, ErrInvalidCredentials) && errors.Is(err, ErrInvalidCredentials) {
					t.Errorf("Login() error = %v, must not be reported as invalid credentials", err)
				}
			default:
				if err != nil {
					t.Fatalf("Login() error = %v, want nil", err)
				}
			}
			if got != tt.want {
				t.Errorf("Login() = %+v, want %+v", got, tt.want)
			}

			findCalls := repo.FindByEmailCalls()
			if len(findCalls) != tt.wantFindCalls {
				t.Fatalf("FindByEmail calls = %d, want %d", len(findCalls), tt.wantFindCalls)
			}
			if len(findCalls) == 1 {
				if findCalls[0].Email != "alice@example.com" {
					t.Errorf("FindByEmail received %q, want the normalized email", findCalls[0].Email)
				}
				if findCalls[0].Ctx.Value(ctxKey{}) != "request-1" {
					t.Error("FindByEmail did not receive the caller's context")
				}
			}

			compareCalls := hasher.CompareCalls()
			switch {
			case tt.wantComparedTo == "" && len(compareCalls) != 0:
				t.Errorf("Compare calls = %d, want 0", len(compareCalls))
			case tt.wantComparedTo != "" && len(compareCalls) != 1:
				t.Errorf("Compare calls = %d, want 1", len(compareCalls))
			case tt.wantComparedTo != "":
				if compareCalls[0].Hash != tt.wantComparedTo || compareCalls[0].Password != tt.input.Password {
					t.Errorf("Compare received (%q, %q), want (%q, %q)",
						compareCalls[0].Hash, compareCalls[0].Password, tt.wantComparedTo, tt.input.Password)
				}
			}

			issueCalls := tokens.IssueCalls()
			if len(issueCalls) != tt.wantIssueCalls {
				t.Fatalf("Issue calls = %d, want %d", len(issueCalls), tt.wantIssueCalls)
			}
			if len(issueCalls) == 1 && issueCalls[0].Subject != stored.ID {
				t.Errorf("Issue received subject %q, want the user ID %q", issueCalls[0].Subject, stored.ID)
			}
		})
	}
}

func TestService_Login_HashesTheDummyPasswordOnce(t *testing.T) {
	t.Parallel()

	repo := &UserRepositoryMock{FindByEmailFunc: func(context.Context, string) (user.User, error) {
		return user.User{}, user.ErrNotFound
	}}
	hasher := &PasswordHasherMock{
		HashFunc:    func(string) (string, error) { return "dummy-hash", nil },
		CompareFunc: func(string, string) (bool, error) { return false, nil },
	}
	service := New(repo, hasher, &TokenIssuerMock{}, time.Now)

	for range 3 {
		if _, err := service.Login(t.Context(), LoginInput{Email: "nobody@example.com", Password: "correct horse"}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
		}
	}

	if n := len(hasher.HashCalls()); n != 1 {
		t.Errorf("Hash calls = %d, want 1 for every login after the first", n)
	}
	if n := len(hasher.CompareCalls()); n != 3 {
		t.Errorf("Compare calls = %d, want one per login", n)
	}
}
