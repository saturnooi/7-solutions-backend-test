package userservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestService_CountUsers(t *testing.T) {
	t.Parallel()

	errDatabase := errors.New("connection reset")

	tests := []struct {
		name    string
		count   func(context.Context) (int64, error)
		want    int64
		wantErr error
	}{
		{name: "number of stored users", count: func(context.Context) (int64, error) { return 42, nil }, want: 42},
		{name: "no users", count: func(context.Context) (int64, error) { return 0, nil }, want: 0},
		{name: "repository failure", count: func(context.Context) (int64, error) { return 0, errDatabase }, wantErr: errDatabase},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &UserRepositoryMock{CountFunc: tt.count}
			service := New(repo, &PasswordHasherMock{}, &TokenIssuerMock{}, time.Now)
			ctx := context.WithValue(t.Context(), ctxKey{}, "request-1")

			got, err := service.CountUsers(ctx)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CountUsers() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("CountUsers() = %d, want %d", got, tt.want)
			}
			calls := repo.CountCalls()
			if len(calls) != 1 || calls[0].Ctx.Value(ctxKey{}) != "request-1" {
				t.Errorf("Count calls = %d, want one call with the caller's context", len(calls))
			}
		})
	}
}
