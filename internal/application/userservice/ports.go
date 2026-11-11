package userservice

import (
	"context"
	"time"

	"user-management-api/internal/domain/user"
)

type UserRepository interface {
	Create(ctx context.Context, u user.User) (user.User, error)
	FindByEmail(ctx context.Context, email string) (user.User, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) (bool, error)
}

type AccessToken struct {
	Value     string
	ExpiresIn time.Duration
}

type TokenIssuer interface {
	Issue(subject string) (AccessToken, error)
}
