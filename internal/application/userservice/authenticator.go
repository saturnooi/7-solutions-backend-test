package userservice

import (
	"context"
	"errors"
	"fmt"

	"user-management-api/internal/domain/user"
)

var ErrUnauthenticated = errors.New("userservice: authentication required")

type Authenticator struct {
	verifier TokenVerifier
	users    UserFinder
}

func NewAuthenticator(verifier TokenVerifier, users UserFinder) *Authenticator {
	return &Authenticator{verifier: verifier, users: users}
}

func (a *Authenticator) Authenticate(ctx context.Context, token string) (string, error) {
	userID, err := a.verifier.Verify(token)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}

	_, err = a.users.FindByID(ctx, userID)
	if errors.Is(err, user.ErrNotFound) || errors.Is(err, user.ErrInvalidID) {
		return "", fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	if err != nil {
		return "", fmt.Errorf("authenticate: %w", err)
	}
	return userID, nil
}
