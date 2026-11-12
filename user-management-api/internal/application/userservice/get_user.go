package userservice

import (
	"context"
	"fmt"

	"user-management-api/internal/domain/user"
)

func (s *Service) GetUser(ctx context.Context, id string) (user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return user.User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}
