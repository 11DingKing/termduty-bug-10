package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"runtime/debug"
	"time"

	"termduty/internal/domain"
	"termduty/internal/orchestration"
)

type ctxKey string

const (
	actorKey ctxKey = "actor"
	reqIDKey ctxKey = "request_id"
)

// actorFromContext returns the actor recorded by the actor middleware.
func actorFromContext(ctx context.Context) orchestration.Actor {
	v, _ := ctx.Value(actorKey).(orchestration.Actor)
	if v.ID == "" {
		return orchestration.Actor{ID: "anonymous", Role: domain.RoleSystem}
	}
	return v
}

func contextWithActor(ctx context.Context, a orchestration.Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// actorMiddleware reads the acting user from headers so every mutation is
// attributed. Missing values default to an anonymous system actor.
func (s *Server) actorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Actor-ID")
		role := domain.Role(r.Header.Get("X-Actor-Role"))
		if id == "" {
			id = "anonymous"
		}
		if !role.Valid() {
			role = domain.RoleSystem
		}
		r = r.WithContext(contextWithActor(r.Context(), orchestration.Actor{ID: id, Role: role}))
		next.ServeHTTP(w, r)
	})
}

// requestIDMiddleware tags each request with a short id for log correlation.
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), reqIDKey, id))
		next.ServeHTTP(w, r)
	})
}

// logMiddleware records access logs with method, path, status and duration.
func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		if s.log != nil {
			s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.status,
				"duration_ms", time.Since(start).Milliseconds(), "request_id", r.Context().Value(reqIDKey))
		}
	})
}

// recoverMiddleware turns panics into 500s with a stack trace in the log.
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if s.log != nil {
					s.log.Error("panic", "recover", rec, "stack", string(debug.Stack()), "path", r.URL.Path)
				}
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// mustRole enforces that the acting user holds the required role. On mismatch it
// writes a 403 and returns ok=false so the handler can return early.
func (s *Server) mustRole(w http.ResponseWriter, r *http.Request, role domain.Role) (orchestration.Actor, bool) {
	actor := actorFromContext(r.Context())
	if actor.Role == role {
		return actor, true
	}
	writeErr(w, r, s.log, domain.ErrValidation)
	return actor, false
}

// decodeRole enforces a role then decodes the JSON body into dst, reporting a
// validation error on malformed input. It is the shared entry for every
// mutating admin/batch handler so the role+decode prelude lives in one place.
func (s *Server) decodeRole(w http.ResponseWriter, r *http.Request, role domain.Role, dst any) (orchestration.Actor, bool) {
	actor, ok := s.mustRole(w, r, role)
	if !ok {
		return actor, false
	}
	if err := decodeJSON(r, dst); err != nil {
		writeErr(w, r, s.log, domain.ErrValidation)
		return actor, false
	}
	return actor, true
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.status = status
	r.wrote = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
