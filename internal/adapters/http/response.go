package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

const maxBodyBytes = 1 << 20

var errTrailingData = errors.New("request body has data after the JSON object")

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string           `json:"code"`
	Message string           `json:"message"`
	Fields  []fieldErrorBody `json:"fields,omitempty"`
}

type fieldErrorBody struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errTrailingData
	}
	return nil
}

func writeDecodeError(w http.ResponseWriter, err error) {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body must not exceed 1 MiB")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a single JSON object with only the documented fields")
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if verr, ok := errors.AsType[*user.ValidationError](err); ok {
		fields := make([]fieldErrorBody, len(verr.Fields))
		for i, f := range verr.Fields {
			fields[i] = fieldErrorBody{Field: f.Field, Message: f.Message}
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: errorBody{
			Code:    "validation_failed",
			Message: "request validation failed",
			Fields:  fields,
		}})
		return
	}
	if errors.Is(err, user.ErrEmailAlreadyExists) {
		writeError(w, http.StatusConflict, "email_already_exists", "email is already registered")
		return
	}
	if errors.Is(err, user.ErrInvalidID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a 24-character hexadecimal string")
		return
	}
	if errors.Is(err, user.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user_not_found", "user not found")
		return
	}
	if errors.Is(err, userservice.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}

	h.logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
