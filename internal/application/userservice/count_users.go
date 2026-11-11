package userservice

import (
	"context"
	"fmt"
)

func (s *Service) CountUsers(ctx context.Context) (int64, error) {
	count, err := s.repo.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}
