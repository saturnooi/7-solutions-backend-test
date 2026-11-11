package http

import "net/http"

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.users.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newUserResponse(u))
}
