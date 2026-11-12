package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const maxPasswordBytes = 72

type BcryptHasher struct {
	cost int
}

func NewBcryptHasher(cost int) *BcryptHasher {
	return &BcryptHasher{cost: cost}
}

func (h *BcryptHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", fmt.Errorf("bcrypt: hash password: %w", err)
	}
	return string(hash), nil
}

func (h *BcryptHasher) Compare(hash, password string) (bool, error) {
	if len(password) > maxPasswordBytes {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, nil
	default:
		return false, fmt.Errorf("bcrypt: compare password: %w", err)
	}
}
