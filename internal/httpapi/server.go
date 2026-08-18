package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"termduty/internal/config"
	"termduty/internal/crosscut"
	"termduty/internal/orchestration"
	"termduty/internal/scheduler"
	"termduty/internal/store"
)

// crosscutLogger lets middleware files call the shared error writer without a
// direct dependency on the handler source files.
type crosscutLogger = slog.Logger

// Server is the HTTP entrypoint. It wires the chi router to the orchestration
// services and the scheduler, and serves the built frontend as static assets.
type Server struct {
	svc     *orchestration.Services
	sched   *scheduler.Scheduler
	cfg     config.Config
	log     *slog.Logger
	store   *store.Store
	router  chi.Router
	httpSrv *http.Server
	ready   atomic.Bool
}

// New assembles the router and the underlying http.Server.
func New(svc *orchestration.Services, sched *scheduler.Scheduler, cfg config.Config, log *slog.Logger, st *store.Store) *Server {
	s := &Server{svc: svc, sched: sched, cfg: cfg, log: log, store: st}
	s.router = s.buildRouter()
	s.httpSrv = &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      s.router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
	return s
}

// Handler returns the configured router, useful for tests.
func (s *Server) Handler() http.Handler { return s.router }

// MarkReady toggles the migration/readiness flag reported by /readyz.
func (s *Server) MarkReady(b bool) { s.ready.Store(b) }

// Start begins listening. It blocks until the server stops.
func (s *Server) Start(_ context.Context) error {
	s.log.Info("http server listening", "addr", s.cfg.HTTPAddr)
	err := s.httpSrv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// writeErr maps an error to a JSON response through the shared crosscut helper.
func writeErr(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	crosscut.WriteError(w, r, log, err)
}

// writeJSON encodes a value as JSON with the correct content type.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON decodes a JSON body into dst, reporting a validation error on
// malformed input.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

// healthz reports only that the process is alive.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// readyz probes the real dependencies: the database must answer a ping, the
// shard directory must be writable, the scheduler must be running and the
// schema must be migrated. Any failure returns 503 naming the failed check.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	type check struct {
		Name string `json:"name"`
		OK   bool   `json:"ok"`
		Err  string `json:"err,omitempty"`
	}
	checks := []check{}
	overall := true

	if err := s.store.Ping(r.Context()); err != nil {
		checks = append(checks, check{Name: "database", OK: false, Err: err.Error()})
		overall = false
	} else {
		checks = append(checks, check{Name: "database", OK: true})
	}

	if !s.sched.Started() {
		checks = append(checks, check{Name: "scheduler", OK: false, Err: "not started"})
		overall = false
	} else {
		checks = append(checks, check{Name: "scheduler", OK: true})
	}

	if !s.ready.Load() {
		checks = append(checks, check{Name: "migration", OK: false, Err: "not ready"})
		overall = false
	} else {
		checks = append(checks, check{Name: "migration", OK: true})
	}

	w.Header().Set("Content-Type", "application/json")
	if !overall {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ready": overall, "checks": checks})
}
