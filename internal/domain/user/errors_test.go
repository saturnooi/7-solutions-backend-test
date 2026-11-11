package user

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields []FieldError
		want   string
	}{
		{
			name:   "one field",
			fields: []FieldError{{Field: "email", Message: "is required"}},
			want:   "invalid user: email is required",
		},
		{
			name: "several fields keep their order",
			fields: []FieldError{
				{Field: "name", Message: "is required"},
				{Field: "password", Message: "must be at least 8 characters"},
			},
			want: "invalid user: name is required; password must be at least 8 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &ValidationError{Fields: tt.fields}

			if got := err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidationError_SurvivesWrapping(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("register user: %w", ValidateNewUser("", validEmail, validPassword))

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("errors.As(%v) = false, want a *ValidationError", err)
	}
	want := []FieldError{{Field: "name", Message: "is required"}}
	if !slices.Equal(ve.Fields, want) {
		t.Errorf("fields = %+v, want %+v", ve.Fields, want)
	}
}
