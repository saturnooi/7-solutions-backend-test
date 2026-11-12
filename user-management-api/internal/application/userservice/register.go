package userservice

import (
	"context"
	"fmt"

	"user-management-api/internal/domain/user"
)

type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (user.User, error) {
	if err := user.ValidateNewUser(in.Name, in.Email, in.Password); err != nil {
		return user.User{}, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return user.User{}, fmt.Errorf("register user: hash password: %w", err)
	}

	u, err := user.New(in.Name, in.Email, hash, s.now())
	if err != nil {
		return user.User{}, fmt.Errorf("register user: %w", err)
	}

	created, err := s.repo.Create(ctx, u)
	if err != nil {
		return user.User{}, fmt.Errorf("register user: %w", err)
	}
	return created, nil
}
