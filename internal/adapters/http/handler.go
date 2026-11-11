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
	Login(ctx context.Context, in userservice.LoginInput) (userservice.AccessToken, error)
}

type Handler struct {
	users  UserService
	tokens TokenVerifier
	logger *slog.Logger
}

func NewHandler(users UserService, tokens TokenVerifier, logger *slog.Logger) *Handler {
	return &Handler{users: users, tokens: tokens, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", h.register)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.Handle("POST /api/v1/users", h.requireAuth(http.HandlerFunc(h.register)))
	return mux
}
