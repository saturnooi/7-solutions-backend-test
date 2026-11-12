package user

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	validName     = "Alice Smith"
	validEmail    = "alice@example.com"
	validPassword = "correct horse"
)

func TestValidateNewUser_Name(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain name", input: "Alice Smith"},
		{name: "surrounding whitespace is ignored", input: "  Alice  "},
		{name: "100 Thai characters (300 bytes) are allowed", input: strings.Repeat("ก", 100)},
		{name: "empty", input: "", want: "is required"},
		{name: "only whitespace", input: " \t\n ", want: "is required"},
		{name: "101 Thai characters", input: strings.Repeat("ก", 101), want: "must be at most 100 characters"},
		{name: "line break inside", input: "Alice\nSmith", want: "must not contain control characters"},
		{name: "NUL byte inside", input: "Alice\x00Smith", want: "must not contain control characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateNewUser(tt.input, validEmail, validPassword)

			assertFieldErrors(t, err, wantField("name", tt.want))
		})
	}
}

func TestValidateNewUser_Email(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain address", input: "alice@example.com"},
		{name: "case and surrounding whitespace are ignored", input: "  Alice@Example.COM  "},
		{name: "plus tag", input: "alice+news@example.com"},
		{name: "Thai address", input: "อลิซ@ตัวอย่าง.ไทย"},
		{name: "254 bytes", input: emailOfLength(254)},
		{name: "empty", input: "", want: "is required"},
		{name: "only whitespace", input: "   ", want: "is required"},
		{name: "255 bytes", input: emailOfLength(255), want: "must be at most 254 bytes"},
		{name: "missing @", input: "alice.example.com", want: "must be a valid email address"},
		{name: "missing domain", input: "alice@", want: "must be a valid email address"},
		{name: "missing local part", input: "@example.com", want: "must be a valid email address"},
		{name: "domain without a dot", input: "alice@gmail", want: "must be a valid email address"},
		{name: "consecutive dots", input: "alice@example..com", want: "must be a valid email address"},
		{name: "space inside", input: "alice smith@example.com", want: "must be a valid email address"},
		{name: "display name", input: "Alice <alice@example.com>", want: "must be a valid email address"},
		{name: "trailing comment", input: "alice@example.com (Alice)", want: "must be a valid email address"},
		{name: "quoted local part", input: `"alice"@example.com`, want: "must be a valid email address"},
		{name: "two addresses", input: "alice@example.com, bob@example.com", want: "must be a valid email address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateNewUser(validName, tt.input, validPassword)

			assertFieldErrors(t, err, wantField("email", tt.want))
		})
	}
}

func TestValidateNewUser_Password(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "8 characters", input: "abcdefgh"},
		{name: "72 bytes", input: strings.Repeat("a", 72)},
		{name: "spaces count and are not trimmed", input: "       a"},
		{name: "24 Thai characters (72 bytes)", input: strings.Repeat("ก", 24)},
		{name: "empty", input: "", want: "is required"},
		{name: "7 characters", input: "abcdefg", want: "must be at least 8 characters"},
		{name: "3 Thai characters (9 bytes) are too few", input: "กขค", want: "must be at least 8 characters"},
		{name: "73 bytes", input: strings.Repeat("a", 73), want: "must be at most 72 bytes"},
		{name: "25 Thai characters (75 bytes)", input: strings.Repeat("ก", 25), want: "must be at most 72 bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateNewUser(validName, validEmail, tt.input)

			assertFieldErrors(t, err, wantField("password", tt.want))
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "already normalized", input: "alice@example.com", want: "alice@example.com"},
		{name: "mixed case with spaces", input: "  Alice@Example.COM \t", want: "alice@example.com"},
		{name: "Thai address is unchanged", input: "อลิซ@ตัวอย่าง.ไทย", want: "อลิซ@ตัวอย่าง.ไทย"},
		{name: "invalid input is only trimmed and lowercased", input: " Not An Email ", want: "not an email"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeEmail(tt.input); got != tt.want {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateNewUser_ReportsEveryInvalidFieldInOrder(t *testing.T) {
	t.Parallel()

	err := ValidateNewUser("", "not-an-email", "short")

	assertFieldErrors(t, err, []FieldError{
		{Field: "name", Message: "is required"},
		{Field: "email", Message: "must be a valid email address"},
		{Field: "password", Message: "must be at least 8 characters"},
	})
}

func TestValidateNewUser_ErrorDoesNotEchoInput(t *testing.T) {
	t.Parallel()

	inputs := []string{"Mallory\x00", "mallory-at-example.com", "hunter2"}

	err := ValidateNewUser(inputs[0], inputs[1], inputs[2])

	if err == nil {
		t.Fatal("ValidateNewUser() error = nil, want a validation error")
	}
	for _, in := range inputs {
		if strings.Contains(err.Error(), in) {
			t.Errorf("error %q contains the submitted value %q", err, in)
		}
	}
}

func wantField(field, message string) []FieldError {
	if message == "" {
		return nil
	}
	return []FieldError{{Field: field, Message: message}}
}

func assertFieldErrors(t *testing.T, err error, want []FieldError) {
	t.Helper()

	if want == nil {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v (%T), want *ValidationError", err, err)
	}
	if !slices.Equal(ve.Fields, want) {
		t.Errorf("fields = %+v, want %+v", ve.Fields, want)
	}
}

func emailOfLength(n int) string {
	const domain = "@example.com"
	return strings.Repeat("a", n-len(domain)) + domain
}
