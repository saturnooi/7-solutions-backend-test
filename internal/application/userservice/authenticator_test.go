package userservice

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"user-management-api/internal/domain/user"
)

func TestAuthenticator_Authenticate(t *testing.T) {
	t.Parallel()

	const (
		token  = "header.payload.signature"
		userID = "6730f1c2a1b2c3d4e5f60718"
	)
	errVerify := errors.New("jwt: invalid token: token is expired")
	errDatabase := errors.New("server selection timeout")
	verifyOK := func(string) (string, error) { return userID, nil }

	tests := []struct {
		name                string
		verify              func(string) (string, error)
		exists              func(context.Context, string) (bool, error)
		want                string
		wantErr             error
		wantUnauthenticated bool
		wantLookup          string
	}{
		{
			name:       "valid token for an existing user",
			verify:     verifyOK,
			exists:     func(context.Context, string) (bool, error) { return true, nil },
			want:       userID,
			wantLookup: userID,
		},
		{
			name:                "token rejected",
			verify:              func(string) (string, error) { return "", errVerify },
			wantErr:             errVerify,
			wantUnauthenticated: true,
		},
		{
			name:                "user deleted after the token was issued",
			verify:              verifyOK,
			exists:              func(context.Context, string) (bool, error) { return false, nil },
			wantErr:             user.ErrNotFound,
			wantUnauthenticated: true,
			wantLookup:          userID,
		},
		{
			name:   "subject is not a valid id",
			verify: func(string) (string, error) { return "not-an-id", nil },
			exists: func(context.Context, string) (bool, error) {
				return false, fmt.Errorf("mongodb: check user exists: %w", user.ErrInvalidID)
			},
			wantErr:             user.ErrInvalidID,
			wantUnauthenticated: true,
			wantLookup:          "not-an-id",
		},
		{
			name:       "lookup failure is not reported as unauthenticated",
			verify:     verifyOK,
			exists:     func(context.Context, string) (bool, error) { return false, errDatabase },
			wantErr:    errDatabase,
			wantLookup: userID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verifier := &TokenVerifierMock{VerifyFunc: tt.verify}
			users := &UserCheckerMock{ExistsFunc: tt.exists}
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := NewAuthenticator(verifier, users).Authenticate(ctx, token)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tt.wantErr)
			}
			if errors.Is(err, ErrUnauthenticated) != tt.wantUnauthenticated {
				t.Errorf("errors.Is(err, ErrUnauthenticated) = %v, want %v (err %v)", !tt.wantUnauthenticated, tt.wantUnauthenticated, err)
			}
			if got != tt.want {
				t.Errorf("Authenticate() = %q, want %q", got, tt.want)
			}

			if calls := verifier.VerifyCalls(); len(calls) != 1 || calls[0].Token != token {
				t.Errorf("Verify calls = %+v, want one call with %q", calls, token)
			}
			lookups := users.ExistsCalls()
			switch {
			case tt.wantLookup == "" && len(lookups) != 0:
				t.Errorf("Exists calls = %d, want 0", len(lookups))
			case tt.wantLookup != "" && (len(lookups) != 1 || lookups[0].ID != tt.wantLookup):
				t.Errorf("Exists calls = %+v, want one call with %q", lookups, tt.wantLookup)
			case tt.wantLookup != "" && lookups[0].Ctx.Value(ctxKey{}) != "request-1":
				t.Error("Exists did not receive the caller's context")
			}
		})
	}
}
