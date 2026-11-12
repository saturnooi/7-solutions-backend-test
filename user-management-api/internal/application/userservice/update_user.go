package userservice

import (
	"context"
	"errors"
	"fmt"

	"user-management-api/internal/domain/user"
)

var ErrForbidden = errors.New("userservice: users may only change their own account")

type UpdateInput struct {
	Name  *string
	Email *string
}

func (s *Service) UpdateUser(ctx context.Context, callerID, id string, in UpdateInput) (user.User, error) {
	if err := authorizeOwner(callerID, id); err != nil {
		return user.User{}, err
	}

	changes, err := user.NewChanges(in.Name, in.Email)
	if err != nil {
		return user.User{}, err
	}

	updated, err := s.repo.Update(ctx, id, changes)
	if err != nil {
		return user.User{}, fmt.Errorf("update user: %w", err)
	}
	return updated, nil
}

func authorizeOwner(callerID, id string) error {
	if callerID == "" || callerID != id {
		return ErrForbidden
	}
	return nil
}
