package userservice

import "time"

type Service struct {
	repo   UserRepository
	hasher PasswordHasher
	now    func() time.Time
}

func New(repo UserRepository, hasher PasswordHasher, now func() time.Time) *Service {
	return &Service{repo: repo, hasher: hasher, now: now}
}
