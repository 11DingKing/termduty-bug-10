package crosscut

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// ErrorReply is the uniform JSON error envelope returned by every handler.
type ErrorReply struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

// MapError converts a domain/store error into an HTTP status and a stable code.
// It uses errors.Is for sentinels and errors.As to surface the winning handler
// when an alert was already accepted by someone else.
func MapError(err error) (int, ErrorReply) {
	if err == nil {
		return http.StatusOK, ErrorReply{Code: "ok"}
	}
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return http.StatusNotFound, ErrorReply{Code: "not_found", Message: notFound.Error()}
	}
	var taken *store.AlreadyAssignedError
	if errors.As(err, &taken) {
		return http.StatusConflict, ErrorReply{Code: "already_assigned", Message: taken.Error(), Detail: map[string]string{"assignee": taken.AssigneeID}}
	}
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		return http.StatusConflict, ErrorReply{Code: "conflict", Message: conflict.Error()}
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, ErrorReply{Code: "not_found", Message: err.Error()}
	case errors.Is(err, domain.ErrIllegalTransition):
		return http.StatusConflict, ErrorReply{Code: "illegal_transition", Message: err.Error()}
	case errors.Is(err, domain.ErrAlreadyAssigned):
		return http.StatusConflict, ErrorReply{Code: "already_assigned", Message: err.Error()}
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, ErrorReply{Code: "conflict", Message: err.Error()}
	case errors.Is(err, domain.ErrSuppressed):
		return http.StatusConflict, ErrorReply{Code: "suppressed", Message: err.Error()}
	case errors.Is(err, domain.ErrAlreadyExists):
		return http.StatusConflict, ErrorReply{Code: "already_exists", Message: err.Error()}
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, ErrorReply{Code: "validation", Message: err.Error()}
	case errors.Is(err, domain.ErrCollectorDisabled):
		return http.StatusBadRequest, ErrorReply{Code: "collector_disabled", Message: err.Error()}
	case errors.Is(err, domain.ErrOutOfRange):
		return http.StatusBadRequest, ErrorReply{Code: "out_of_range", Message: err.Error()}
	case errors.Is(err, domain.ErrSchemaIncompatible):
		return http.StatusServiceUnavailable, ErrorReply{Code: "schema_incompatible", Message: err.Error()}
	}
	return http.StatusInternalServerError, ErrorReply{Code: "internal", Message: err.Error()}
}

// WriteError writes a mapped JSON error response.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	status, reply := MapError(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if log != nil && status >= 500 {
		log.Error("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "err", err)
	}
	_ = json.NewEncoder(w).Encode(reply)
}
