package http

import (
	"context"
	"log/slog"
	"net/http"

	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

type UserService interface {
	Register(ctx context.Context, in userservice.RegisterInput) (user.User, error)
}

type Handler struct {
	users  UserService
	logger *slog.Logger
}

func NewHandler(users UserService, logger *slog.Logger) *Handler {
	return &Handler{users: users, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", h.register)
	return mux
}
