package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"user-management-api/internal/application/userservice"
)

const (
	minSecretBytes = 32
	clockLeeway    = 5 * time.Second
)

var (
	errEmptySubject = errors.New("jwt: subject is required")
	errWeakSecret   = fmt.Errorf("jwt: secret must be at least %d bytes", minSecretBytes)
	errInvalidToken = errors.New("jwt: invalid token")
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

func (s *JWTService) Verify(token string) (string, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockLeeway),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidToken, err)
	}
	if claims.Subject == "" {
		return "", fmt.Errorf("%w: subject is missing", errInvalidToken)
	}
	return claims.Subject, nil
}
