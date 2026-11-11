package userservice

import (
	"context"
	"fmt"

	"user-management-api/internal/domain/user"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

type ListInput struct {
	Limit  int
	Offset int
}

type UserPage struct {
	Users  []user.User
	Limit  int
	Offset int
}

func (s *Service) ListUsers(ctx context.Context, in ListInput) (UserPage, error) {
	if err := validateList(in); err != nil {
		return UserPage{}, err
	}

	users, err := s.repo.List(ctx, in.Limit, in.Offset)
	if err != nil {
		return UserPage{}, fmt.Errorf("list users: %w", err)
	}
	return UserPage{Users: users, Limit: in.Limit, Offset: in.Offset}, nil
}

func validateList(in ListInput) error {
	var fields []user.FieldError
	if in.Limit < 1 || in.Limit > MaxPageSize {
		fields = append(fields, user.FieldError{Field: "limit", Message: fmt.Sprintf("must be between 1 and %d", MaxPageSize)})
	}
	if in.Offset < 0 {
		fields = append(fields, user.FieldError{Field: "offset", Message: "must not be negative"})
	}
	if len(fields) == 0 {
		return nil
	}
	return &user.ValidationError{Fields: fields}
}
