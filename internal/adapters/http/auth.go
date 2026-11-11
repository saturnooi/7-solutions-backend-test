package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"user-management-api/internal/application/userservice"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (string, error)
}

type userIDKey struct{}

func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeUnauthorized(w, "Bearer")
			return
		}
		userID, err := h.auth.Authenticate(r.Context(), token)
		if errors.Is(err, userservice.ErrUnauthenticated) {
			writeUnauthorized(w, `Bearer error="invalid_token"`)
			return
		}
		if err != nil {
			h.writeServiceError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey{}, userID)))
	})
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func userIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey{}).(string)
	return userID, ok
}

func writeUnauthorized(w http.ResponseWriter, challenge string) {
	w.Header().Set("WWW-Authenticate", challenge)
	writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
}
