package user

import (
	"errors"
	"strings"
)

type FieldError struct {
	Field   string
	Message string
}

type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + " " + f.Message
	}
	return "invalid user: " + strings.Join(parts, "; ")
}

func validationError(checks ...FieldError) error {
	var failed []FieldError
	for _, c := range checks {
		if c.Message != "" {
			failed = append(failed, c)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return &ValidationError{Fields: failed}
}

var (
	ErrEmailAlreadyExists = errors.New("user: email already exists")
	ErrNotFound           = errors.New("user: not found")
	ErrInvalidID          = errors.New("user: invalid id")
	ErrNoChanges          = errors.New("user: no changes")
)

var (
	errMissingPasswordHash = errors.New("user: password hash is required")
	errMissingCreatedAt    = errors.New("user: creation time is required")
)
