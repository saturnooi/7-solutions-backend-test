package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnexpected(t *testing.T) {
	t.Parallel()

	errDatabase := errors.New("mongodb: find user by id: connection reset")
	canceledCtx := func(t *testing.T) context.Context {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		return ctx
	}
	expiredCtx := func(t *testing.T) context.Context {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		t.Cleanup(cancel)
		return ctx
	}

	tests := []struct {
		name      string
		ctx       func(t *testing.T) context.Context
		err       error
		wantCode  codes.Code
		wantLevel string
		wantMsg   string
	}{
		{
			name:      "live call is an internal error",
			ctx:       func(t *testing.T) context.Context { return t.Context() },
			err:       errDatabase,
			wantCode:  codes.Internal,
			wantLevel: "ERROR",
			wantMsg:   "rpc failed",
		},
		{
			name:      "call canceled by the client",
			ctx:       canceledCtx,
			err:       fmt.Errorf("mongodb: find user by id: %w", context.Canceled),
			wantCode:  codes.Canceled,
			wantLevel: "INFO",
			wantMsg:   "rpc canceled",
		},
		{
			name:      "call past its deadline",
			ctx:       expiredCtx,
			err:       fmt.Errorf("mongodb: find user by id: %w", context.DeadlineExceeded),
			wantCode:  codes.DeadlineExceeded,
			wantLevel: "INFO",
			wantMsg:   "rpc canceled",
		},
		{
			name:      "deadline passed but the call failed for another reason",
			ctx:       expiredCtx,
			err:       errDatabase,
			wantCode:  codes.Internal,
			wantLevel: "ERROR",
			wantMsg:   "rpc failed",
		},
		{
			name:      "client canceled while the database timed out on its own",
			ctx:       canceledCtx,
			err:       fmt.Errorf("mongodb: find user by id: %w", context.DeadlineExceeded),
			wantCode:  codes.Internal,
			wantLevel: "ERROR",
			wantMsg:   "rpc failed",
		},
		{
			name:      "a cancellation inside the server while the client still waits",
			ctx:       func(t *testing.T) context.Context { return t.Context() },
			err:       fmt.Errorf("mongodb: find user by id: %w", context.Canceled),
			wantCode:  codes.Internal,
			wantLevel: "ERROR",
			wantMsg:   "rpc failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := &lockedBuffer{}

			err := unexpected(tt.ctx(t), slog.New(slog.NewJSONHandler(logs, nil)), "/user.v1.UserService/GetUser", tt.err)

			if got := status.Code(err); got != tt.wantCode {
				t.Errorf("code = %v, want %v", got, tt.wantCode)
			}
			if strings.Contains(status.Convert(err).Message(), "connection reset") {
				t.Errorf("message leaks the cause: %q", status.Convert(err).Message())
			}
			line := logs.String()
			if !strings.Contains(line, `"level":"`+tt.wantLevel+`"`) || !strings.Contains(line, `"msg":"`+tt.wantMsg+`"`) {
				t.Errorf("log = %s, want %s %q", line, tt.wantLevel, tt.wantMsg)
			}
		})
	}
}
