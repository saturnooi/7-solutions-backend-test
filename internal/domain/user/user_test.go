package user

import (
	"errors"
	"testing"
	"time"
)

const testHash = "$2a$10$hash-produced-by-the-hasher"

func TestNew(t *testing.T) {
	t.Parallel()

	ict := time.FixedZone("ICT", 7*60*60)

	tests := []struct {
		name      string
		inName    string
		inEmail   string
		createdAt time.Time
		want      User
	}{
		{
			name:      "trims the name and normalizes the email",
			inName:    "  Alice Smith  ",
			inEmail:   "  Alice@Example.COM ",
			createdAt: time.Date(2026, time.November, 10, 8, 0, 0, 0, time.UTC),
			want: User{
				Name:         "Alice Smith",
				Email:        "alice@example.com",
				PasswordHash: testHash,
				CreatedAt:    time.Date(2026, time.November, 10, 8, 0, 0, 0, time.UTC),
			},
		},
		{
			name:      "stores the creation time in UTC at millisecond precision",
			inName:    validName,
			inEmail:   validEmail,
			createdAt: time.Date(2026, time.November, 10, 15, 4, 5, 123_456_789, ict),
			want: User{
				Name:         validName,
				Email:        validEmail,
				PasswordHash: testHash,
				CreatedAt:    time.Date(2026, time.November, 10, 8, 4, 5, 123_000_000, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := New(tt.inName, tt.inEmail, testHash, tt.createdAt)

			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if got.ID != "" {
				t.Errorf("ID = %q, want empty until the user is stored", got.ID)
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Email != tt.want.Email {
				t.Errorf("Email = %q, want %q", got.Email, tt.want.Email)
			}
			if got.PasswordHash != tt.want.PasswordHash {
				t.Errorf("PasswordHash = %q, want %q", got.PasswordHash, tt.want.PasswordHash)
			}
			if !got.CreatedAt.Equal(tt.want.CreatedAt) || got.CreatedAt.Location() != time.UTC {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, tt.want.CreatedAt)
			}
		})
	}
}

func TestNew_RejectsInvalidNameOrEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		inName  string
		inEmail string
		want    []FieldError
	}{
		{
			name:    "invalid name",
			inName:  "   ",
			inEmail: validEmail,
			want:    []FieldError{{Field: "name", Message: "is required"}},
		},
		{
			name:    "invalid email",
			inName:  validName,
			inEmail: "Alice <alice@example.com>",
			want:    []FieldError{{Field: "email", Message: "must be a valid email address"}},
		},
		{
			name:    "both invalid",
			inName:  "",
			inEmail: "",
			want: []FieldError{
				{Field: "name", Message: "is required"},
				{Field: "email", Message: "is required"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := New(tt.inName, tt.inEmail, testHash, time.Now())

			assertFieldErrors(t, err, tt.want)
			if got != (User{}) {
				t.Errorf("New() = %+v, want the zero User on error", got)
			}
		})
	}
}

func TestNew_RejectsMissingHashOrTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		hash      string
		createdAt time.Time
		wantErr   error
	}{
		{name: "empty password hash", hash: "", createdAt: time.Now(), wantErr: errMissingPasswordHash},
		{name: "zero creation time", hash: testHash, createdAt: time.Time{}, wantErr: errMissingCreatedAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := New(validName, validEmail, tt.hash, tt.createdAt)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantErr)
			}
			var ve *ValidationError
			if errors.As(err, &ve) {
				t.Errorf("New() error is a *ValidationError, want an internal error")
			}
			if got != (User{}) {
				t.Errorf("New() = %+v, want the zero User on error", got)
			}
		})
	}
}
