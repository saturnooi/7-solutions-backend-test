package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret = "test-secret-that-is-at-least-32-bytes-long"
	testIssuer = "user-management-api"
	testUserID = "6730f1c2a1b2c3d4e5f60718"
)

func TestNewJWTService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		secret  []byte
		wantErr error
	}{
		{name: "32-byte secret", secret: []byte(strings.Repeat("s", 32))},
		{name: "31-byte secret", secret: []byte(strings.Repeat("s", 31)), wantErr: errWeakSecret},
		{name: "empty secret", secret: nil, wantErr: errWeakSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, err := NewJWTService(tt.secret, testIssuer, time.Hour, time.Now)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewJWTService() error = %v, want %v", err, tt.wantErr)
			}
			if (service == nil) != (tt.wantErr != nil) {
				t.Errorf("NewJWTService() service = %v, want nil only on error", service)
			}
		})
	}
}

func TestJWTService_Issue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 11, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{name: "one hour", ttl: time.Hour},
		{name: "fifteen minutes", ttl: 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := newTestJWTService(t, tt.ttl, func() time.Time { return now })

			got, err := service.Issue(testUserID)

			if err != nil {
				t.Fatalf("Issue() error = %v, want nil", err)
			}
			if got.ExpiresIn != tt.ttl {
				t.Errorf("ExpiresIn = %v, want %v", got.ExpiresIn, tt.ttl)
			}

			var claims jwt.RegisteredClaims
			token, err := jwt.ParseWithClaims(got.Value, &claims,
				func(*jwt.Token) (any, error) { return []byte(testSecret), nil },
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithTimeFunc(func() time.Time { return now }),
			)
			if err != nil {
				t.Fatalf("token does not verify: %v", err)
			}
			if alg := token.Header["alg"]; alg != "HS256" {
				t.Errorf("alg = %v, want HS256", alg)
			}
			if claims.Subject != testUserID {
				t.Errorf("sub = %q, want %q", claims.Subject, testUserID)
			}
			if claims.Issuer != testIssuer {
				t.Errorf("iss = %q, want %q", claims.Issuer, testIssuer)
			}
			if claims.IssuedAt == nil || !claims.IssuedAt.Equal(now) {
				t.Errorf("iat = %v, want %v", claims.IssuedAt, now)
			}
			if claims.ExpiresAt == nil || !claims.ExpiresAt.Equal(now.Add(tt.ttl)) {
				t.Errorf("exp = %v, want %v", claims.ExpiresAt, now.Add(tt.ttl))
			}
		})
	}
}

func TestJWTService_Issue_TokenFailsVerification(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.November, 11, 9, 30, 0, 0, time.UTC)
	token, err := newTestJWTService(t, time.Hour, func() time.Time { return issuedAt }).Issue(testUserID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	tests := []struct {
		name    string
		secret  string
		at      time.Time
		wantErr error
	}{
		{name: "with another secret", secret: "another-secret-that-is-at-least-32-bytes", at: issuedAt, wantErr: jwt.ErrTokenSignatureInvalid},
		{name: "after it expires", secret: testSecret, at: issuedAt.Add(time.Hour + time.Second), wantErr: jwt.ErrTokenExpired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := jwt.Parse(token.Value,
				func(*jwt.Token) (any, error) { return []byte(tt.secret), nil },
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithTimeFunc(func() time.Time { return tt.at }),
			)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Parse() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestJWTService_Issue_RejectsEmptySubject(t *testing.T) {
	t.Parallel()

	got, err := newTestJWTService(t, time.Hour, time.Now).Issue("")

	if !errors.Is(err, errEmptySubject) {
		t.Fatalf("Issue() error = %v, want %v", err, errEmptySubject)
	}
	if got.Value != "" {
		t.Errorf("Issue() = %q, want no token", got.Value)
	}
}

func newTestJWTService(t *testing.T, ttl time.Duration, now func() time.Time) *JWTService {
	t.Helper()

	service, err := NewJWTService([]byte(testSecret), testIssuer, ttl, now)
	if err != nil {
		t.Fatalf("NewJWTService() error = %v", err)
	}
	return service
}
