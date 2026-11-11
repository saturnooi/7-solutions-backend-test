package userservice

import (
	"context"
	"time"

	"user-management-api/internal/domain/user"
)

type UserRepository interface {
	Create(ctx context.Context, u user.User) (user.User, error)
	FindByEmail(ctx context.Context, email string) (user.User, error)
	FindByID(ctx context.Context, id string) (user.User, error)
	List(ctx context.Context, limit, offset int) ([]user.User, error)
	Update(ctx context.Context, id string, changes user.Changes) (user.User, error)
	Delete(ctx context.Context, id string) error
}

type UserFinder interface {
	FindByID(ctx context.Context, id string) (user.User, error)
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

type TokenVerifier interface {
	Verify(token string) (string, error)
}
