package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type ctxKey struct{}

func TestUserCountLogger_Run(t *testing.T) {
	t.Parallel()

	errDatabase := errors.New("count users: mongodb: count users: connection reset")

	tests := []struct {
		name      string
		count     func(ctx context.Context, call int) (int64, error)
		runFor    time.Duration
		want      []string
		wantCalls int
	}{
		{
			name:      "logs the count every interval",
			count:     func(_ context.Context, call int) (int64, error) { return int64(call + 2), nil },
			runFor:    35 * time.Second,
			want:      []string{"INFO user count worker started 10s", "INFO user count 3", "INFO user count 4", "INFO user count 5", "INFO user count worker stopped"},
			wantCalls: 3,
		},
		{
			name:      "nothing is counted before the first interval",
			count:     func(context.Context, int) (int64, error) { return 1, nil },
			runFor:    10*time.Second - time.Nanosecond,
			want:      []string{"INFO user count worker started 10s", "INFO user count worker stopped"},
			wantCalls: 0,
		},
		{
			name: "a failed count is logged and the worker keeps going",
			count: func(_ context.Context, call int) (int64, error) {
				if call == 1 {
					return 0, errDatabase
				}
				return 7, nil
			},
			runFor: 25 * time.Second,
			want: []string{
				"INFO user count worker started 10s",
				"ERROR count users failed " + errDatabase.Error(),
				"INFO user count 7",
				"INFO user count worker stopped",
			},
			wantCalls: 2,
		},
		{
			name: "a count cut short by shutdown is not an error",
			count: func(ctx context.Context, _ int) (int64, error) {
				<-ctx.Done()
				return 0, ctx.Err()
			},
			runFor:    15 * time.Second,
			want:      []string{"INFO user count worker started 10s", "INFO user count worker stopped"},
			wantCalls: 1,
		},
		{
			name: "a real failure during shutdown is still an error",
			count: func(ctx context.Context, _ int) (int64, error) {
				<-ctx.Done()
				return 0, errDatabase
			},
			runFor:    15 * time.Second,
			want:      []string{"INFO user count worker started 10s", "ERROR count users failed " + errDatabase.Error(), "INFO user count worker stopped"},
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				calls := 0
				counter := &UserCounterMock{CountUsersFunc: func(ctx context.Context) (int64, error) {
					calls++
					return tt.count(ctx, calls)
				}}
				logs := &lockedBuffer{}
				w := NewUserCountLogger(counter, 10*time.Second, slog.New(slog.NewJSONHandler(logs, nil)))
				ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "worker"))
				done := make(chan struct{})

				go func() {
					w.Run(ctx)
					close(done)
				}()
				time.Sleep(tt.runFor)
				cancel()
				select {
				case <-done:
				case <-time.After(time.Minute):
					t.Fatal("Run did not return after its context was cancelled")
				}

				if got := logs.summary(t); !slices.Equal(got, tt.want) {
					t.Errorf("logs =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tt.want, "\n  "))
				}
				recorded := counter.CountUsersCalls()
				if len(recorded) != tt.wantCalls {
					t.Fatalf("CountUsers calls = %d, want %d", len(recorded), tt.wantCalls)
				}
				for i, c := range recorded {
					if c.Ctx.Value(ctxKey{}) != "worker" {
						t.Errorf("CountUsers call %d did not receive the worker's context", i)
					}
				}
			})
		})
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) summary(t *testing.T) []string {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		parts := []string{fmt.Sprint(entry["level"]), fmt.Sprint(entry["msg"])}
		for _, key := range []string{"interval", "count", "error"} {
			if v, ok := entry[key]; ok {
				parts = append(parts, fmt.Sprint(v))
			}
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}
