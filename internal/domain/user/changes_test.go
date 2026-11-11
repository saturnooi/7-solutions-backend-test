package user

import (
	"errors"
	"testing"
)

func TestNewChanges(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	tests := []struct {
		name       string
		inName     *string
		inEmail    *string
		wantName   *string
		wantEmail  *string
		wantErr    error
		wantFields []FieldError
	}{
		{name: "name only is trimmed", inName: ptr("  Alice Cooper "), wantName: ptr("Alice Cooper")},
		{name: "email only is normalized", inEmail: ptr(" Alice@New.Example.COM "), wantEmail: ptr("alice@new.example.com")},
		{name: "both", inName: ptr("Alice Cooper"), inEmail: ptr("alice@new.example.com"), wantName: ptr("Alice Cooper"), wantEmail: ptr("alice@new.example.com")},
		{name: "nothing to change", wantErr: ErrNoChanges},
		{name: "blank name", inName: ptr("   "), wantFields: []FieldError{{Field: "name", Message: "is required"}}},
		{name: "invalid email", inEmail: ptr("alice@localhost"), wantFields: []FieldError{{Field: "email", Message: "must be a valid email address"}}},
		{
			name:    "both invalid",
			inName:  ptr("Alice\nCooper"),
			inEmail: ptr(""),
			wantFields: []FieldError{
				{Field: "name", Message: "must not contain control characters"},
				{Field: "email", Message: "is required"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewChanges(tt.inName, tt.inEmail)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewChanges() error = %v, want %v", err, tt.wantErr)
				}
			case tt.wantFields != nil:
				assertFieldErrors(t, err, tt.wantFields)
			default:
				if err != nil {
					t.Fatalf("NewChanges() error = %v, want nil", err)
				}
			}
			if !samePointee(got.Name, tt.wantName) || !samePointee(got.Email, tt.wantEmail) {
				t.Errorf("NewChanges() = {Name: %v, Email: %v}, want {Name: %v, Email: %v}",
					show(got.Name), show(got.Email), show(tt.wantName), show(tt.wantEmail))
			}
		})
	}
}

func samePointee(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func show(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
