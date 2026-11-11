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
	GetUser(ctx context.Context, id string) (user.User, error)
	ListUsers(ctx context.Context, in userservice.ListInput) (userservice.UserPage, error)
	UpdateUser(ctx context.Context, callerID, id string, in userservice.UpdateInput) (user.User, error)
	DeleteUser(ctx context.Context, callerID, id string) error
}

type Handler struct {
	users  UserService
	auth   Authenticator
	health HealthChecker
	logger *slog.Logger
}

func NewHandler(users UserService, auth Authenticator, health HealthChecker, logger *slog.Logger) *Handler {
	return &Handler{users: users, auth: auth, health: health, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("POST /api/v1/auth/register", h.register)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.Handle("POST /api/v1/users", h.requireAuth(http.HandlerFunc(h.register)))
	mux.Handle("GET /api/v1/users", h.requireAuth(http.HandlerFunc(h.listUsers)))
	mux.Handle("GET /api/v1/users/{id}", h.requireAuth(http.HandlerFunc(h.getUser)))
	mux.Handle("PATCH /api/v1/users/{id}", h.requireAuth(http.HandlerFunc(h.updateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", h.requireAuth(http.HandlerFunc(h.deleteUser)))
	return h.logRequests(mux)
}
