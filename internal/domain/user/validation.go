package user

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNameRunes     = 100
	maxEmailBytes    = 254
	minPasswordRunes = 8
	maxPasswordBytes = 72
)

func ValidateNewUser(name, email, password string) error {
	_, nameProblem := normalizeName(name)
	_, emailProblem := normalizeEmail(email)
	return validationError(
		FieldError{Field: "name", Message: nameProblem},
		FieldError{Field: "email", Message: emailProblem},
		FieldError{Field: "password", Message: passwordProblem(password)},
	)
}

func normalizeName(raw string) (name, problem string) {
	name = strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", "is required"
	case utf8.RuneCountInString(name) > maxNameRunes:
		return "", fmt.Sprintf("must be at most %d characters", maxNameRunes)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", "must not contain control characters"
	}
	return name, ""
}

func normalizeEmail(raw string) (email, problem string) {
	email = strings.ToLower(strings.TrimSpace(raw))
	switch {
	case email == "":
		return "", "is required"
	case len(email) > maxEmailBytes:
		return "", fmt.Sprintf("must be at most %d bytes", maxEmailBytes)
	case !isBareAddress(email) || !hasDottedDomain(email):
		return "", "must be a valid email address"
	}
	return email, ""
}

func isBareAddress(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

func hasDottedDomain(email string) bool {
	domain := email[strings.LastIndexByte(email, '@')+1:]
	return strings.Contains(domain, ".")
}

func passwordProblem(password string) string {
	switch {
	case password == "":
		return "is required"
	case utf8.RuneCountInString(password) < minPasswordRunes:
		return fmt.Sprintf("must be at least %d characters", minPasswordRunes)
	case len(password) > maxPasswordBytes:
		return fmt.Sprintf("must be at most %d bytes", maxPasswordBytes)
	}
	return ""
}
