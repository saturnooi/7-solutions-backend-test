package user

import "time"

type User struct {
	ID           string
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

func New(name, email, passwordHash string, createdAt time.Time) (User, error) {
	if passwordHash == "" {
		return User{}, errMissingPasswordHash
	}
	if createdAt.IsZero() {
		return User{}, errMissingCreatedAt
	}

	name, nameProblem := normalizeName(name)
	email, emailProblem := normalizeEmail(email)
	if err := validationError(
		FieldError{Field: "name", Message: nameProblem},
		FieldError{Field: "email", Message: emailProblem},
	); err != nil {
		return User{}, err
	}

	return User{
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    createdAt.UTC().Truncate(time.Millisecond),
	}, nil
}
