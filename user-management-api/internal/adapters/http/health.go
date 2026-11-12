package http

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const healthTimeout = 2 * time.Second

type HealthChecker interface {
	Check(ctx context.Context) error
}

type healthResponse struct {
	Status string `json:"status"`
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()

	if err := h.health.Check(ctx); err != nil {
		if errors.Is(err, r.Context().Err()) {
			h.logger.InfoContext(r.Context(), "health check canceled", "error", err)
		} else {
			h.logger.ErrorContext(r.Context(), "health check failed", "error", err)
		}
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
