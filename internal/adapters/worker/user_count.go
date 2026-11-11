package worker

import (
	"context"
	"log/slog"
	"time"
)

type UserCounter interface {
	CountUsers(ctx context.Context) (int64, error)
}

type UserCountLogger struct {
	users    UserCounter
	interval time.Duration
	logger   *slog.Logger
}

func NewUserCountLogger(users UserCounter, interval time.Duration, logger *slog.Logger) *UserCountLogger {
	return &UserCountLogger{users: users, interval: interval, logger: logger}
}

func (w *UserCountLogger) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "user count worker started", "interval", w.interval.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "user count worker stopped")
			return
		case <-ticker.C:
			w.logCount(ctx)
		}
	}
}

func (w *UserCountLogger) logCount(ctx context.Context) {
	count, err := w.users.CountUsers(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "count users failed", "error", err)
		}
		return
	}
	w.logger.InfoContext(ctx, "user count", "count", count)
}
