package userservice

import (
	"context"
	"errors"
	"fmt"

	"user-management-api/internal/domain/user"
)

const dummyPassword = "password used only to equalize login timing"

var ErrInvalidCredentials = errors.New("userservice: invalid email or password")

type LoginInput struct {
	Email    string
	Password string
}

func (s *Service) Login(ctx context.Context, in LoginInput) (AccessToken, error) {
	email := user.NormalizeEmail(in.Email)
	if err := validateLogin(email, in.Password); err != nil {
		return AccessToken{}, err
	}

	u, err := s.repo.FindByEmail(ctx, email)
	if errors.Is(err, user.ErrNotFound) {
		if err := s.compareWithDummyHash(in.Password); err != nil {
			return AccessToken{}, fmt.Errorf("login: %w", err)
		}
		return AccessToken{}, ErrInvalidCredentials
	}
	if err != nil {
		return AccessToken{}, fmt.Errorf("login: %w", err)
	}

	match, err := s.hasher.Compare(u.PasswordHash, in.Password)
	if err != nil {
		return AccessToken{}, fmt.Errorf("login: compare password: %w", err)
	}
	if !match {
		return AccessToken{}, ErrInvalidCredentials
	}

	token, err := s.tokens.Issue(u.ID)
	if err != nil {
		return AccessToken{}, fmt.Errorf("login: issue token: %w", err)
	}
	return token, nil
}

func validateLogin(email, password string) error {
	var fields []user.FieldError
	if email == "" {
		fields = append(fields, user.FieldError{Field: "email", Message: "is required"})
	}
	if password == "" {
		fields = append(fields, user.FieldError{Field: "password", Message: "is required"})
	}
	if len(fields) == 0 {
		return nil
	}
	return &user.ValidationError{Fields: fields}
}

func (s *Service) compareWithDummyHash(password string) error {
	s.dummyOnce.Do(func() {
		s.dummyHash, s.dummyErr = s.hasher.Hash(dummyPassword)
	})
	if s.dummyErr != nil {
		return fmt.Errorf("hash dummy password: %w", s.dummyErr)
	}
	if _, err := s.hasher.Compare(s.dummyHash, password); err != nil {
		return fmt.Errorf("compare dummy password: %w", err)
	}
	return nil
}
