package userservice

import (
	"context"
	"fmt"
)

func (s *Service) DeleteUser(ctx context.Context, callerID, id string) error {
	if err := authorizeOwner(callerID, id); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}
