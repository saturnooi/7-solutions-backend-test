package http

import (
	"net/http"
	"net/url"
	"strconv"

	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

type userListResponse struct {
	Users  []userResponse `json:"users"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.users.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newUserResponse(u))
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	in, err := parseListQuery(r.URL.Query())
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	page, err := h.users.ListUsers(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	users := make([]userResponse, len(page.Users))
	for i, u := range page.Users {
		users[i] = newUserResponse(u)
	}
	writeJSON(w, http.StatusOK, userListResponse{Users: users, Limit: page.Limit, Offset: page.Offset})
}

func parseListQuery(query url.Values) (userservice.ListInput, error) {
	in := userservice.ListInput{Limit: userservice.DefaultPageSize}
	var fields []user.FieldError
	for _, param := range []struct {
		name string
		dst  *int
	}{
		{name: "limit", dst: &in.Limit},
		{name: "offset", dst: &in.Offset},
	} {
		raw := query.Get(param.name)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			fields = append(fields, user.FieldError{Field: param.name, Message: "must be an integer"})
			continue
		}
		*param.dst = n
	}
	if len(fields) > 0 {
		return in, &user.ValidationError{Fields: fields}
	}
	return in, nil
}
