package userservice

import (
	"context"

	"user-management-api/internal/domain/user"
)

type UserRepository interface {
	Create(ctx context.Context, u user.User) (user.User, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
}
