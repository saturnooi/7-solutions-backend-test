package http

import (
	"net/http"

	"user-management-api/internal/application/userservice"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}

	token, err := h.users.Login(r.Context(), userservice.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: token.Value,
		TokenType:   "Bearer",
		ExpiresIn:   int64(token.ExpiresIn.Seconds()),
	})
}
