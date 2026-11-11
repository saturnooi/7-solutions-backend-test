package userservice

import (
	"sync"
	"time"
)

type Service struct {
	repo   UserRepository
	hasher PasswordHasher
	tokens TokenIssuer
	now    func() time.Time

	dummyOnce sync.Once
	dummyHash string
	dummyErr  error
}

func New(repo UserRepository, hasher PasswordHasher, tokens TokenIssuer, now func() time.Time) *Service {
	return &Service{repo: repo, hasher: hasher, tokens: tokens, now: now}
}
