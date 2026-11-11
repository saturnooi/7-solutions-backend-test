package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"user-management-api/internal/application/userservice"
)

const minSecretBytes = 32

var (
	errEmptySubject = errors.New("jwt: subject is required")
	errWeakSecret   = fmt.Errorf("jwt: secret must be at least %d bytes", minSecretBytes)
)

type JWTService struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

func NewJWTService(secret []byte, issuer string, ttl time.Duration, now func() time.Time) (*JWTService, error) {
	if len(secret) < minSecretBytes {
		return nil, errWeakSecret
	}
	return &JWTService{secret: secret, issuer: issuer, ttl: ttl, now: now}, nil
}

func (s *JWTService) Issue(subject string) (userservice.AccessToken, error) {
	if subject == "" {
		return userservice.AccessToken{}, errEmptySubject
	}

	now := s.now()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    s.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return userservice.AccessToken{}, fmt.Errorf("jwt: sign token: %w", err)
	}
	return userservice.AccessToken{Value: signed, ExpiresIn: s.ttl}, nil
}
