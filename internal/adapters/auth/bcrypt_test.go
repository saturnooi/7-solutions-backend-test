package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBcryptHasher_Hash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "ASCII password", password: "correct horse"},
		{name: "Thai password", password: "รหัสผ่านลับมาก"},
		{name: "72-byte password", password: strings.Repeat("a", 72)},
		{name: "73-byte password", password: strings.Repeat("a", 73), wantErr: bcrypt.ErrPasswordTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash, err := NewBcryptHasher(bcrypt.MinCost).Hash(tt.password)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Hash() error = %v, want %v", err, tt.wantErr)
				}
				if hash != "" {
					t.Errorf("Hash() = %q, want empty on error", hash)
				}
				return
			}
			if err != nil {
				t.Fatalf("Hash() error = %v, want nil", err)
			}
			if strings.Contains(hash, tt.password) {
				t.Errorf("Hash() = %q contains the plain password", hash)
			}
			if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(tt.password)); err != nil {
				t.Errorf("hash does not verify against the password: %v", err)
			}
			if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != bcrypt.MinCost {
				t.Errorf("hash cost = %d (err %v), want %d", cost, err, bcrypt.MinCost)
			}
		})
	}
}

func TestBcryptHasher_Hash_SaltsEveryHash(t *testing.T) {
	t.Parallel()

	hasher := NewBcryptHasher(bcrypt.MinCost)

	first, err1 := hasher.Hash("correct horse")
	second, err2 := hasher.Hash("correct horse")

	if err1 != nil || err2 != nil {
		t.Fatalf("Hash() errors = %v, %v, want nil", err1, err2)
	}
	if first == second {
		t.Errorf("two hashes of the same password are identical: %q", first)
	}
}
