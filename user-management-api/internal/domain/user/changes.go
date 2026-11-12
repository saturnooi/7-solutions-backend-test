package user

type Changes struct {
	Name  *string
	Email *string
}

func NewChanges(name, email *string) (Changes, error) {
	if name == nil && email == nil {
		return Changes{}, ErrNoChanges
	}

	var changes Changes
	var nameProblem, emailProblem string
	if name != nil {
		normalized, problem := normalizeName(*name)
		changes.Name, nameProblem = &normalized, problem
	}
	if email != nil {
		normalized, problem := normalizeEmail(*email)
		changes.Email, emailProblem = &normalized, problem
	}
	if err := validationError(
		FieldError{Field: "name", Message: nameProblem},
		FieldError{Field: "email", Message: emailProblem},
	); err != nil {
		return Changes{}, err
	}
	return changes, nil
}
