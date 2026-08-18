package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// buildRouter mounts the API routes and the single-page frontend. The API
// routes carry real business semantics; the frontend is served as static assets
// with an index.html fallback for client-side routing.
func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(s.recoverMiddleware)
	r.Use(s.requestIDMiddleware)
	r.Use(s.logMiddleware)
	r.Use(s.actorMiddleware)

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	r.Route("/api", func(r chi.Router) {
		r.Post("/ingest", s.handleIngest)

		r.Get("/readings", s.handleListReadings)
		r.Get("/readings/export", s.handleExportReadings)
		r.Get("/readings/{id}", s.handleGetReading)

		r.Get("/alerts", s.handleListAlerts)
		r.Get("/alerts/{id}", s.handleGetAlert)
		r.Post("/alerts/{id}/accept", s.handleAcceptAlert)
		r.Post("/alerts/{id}/start", s.handleStartAlert)
		r.Post("/alerts/{id}/resolve", s.handleResolveAlert)
		r.Post("/alerts/{id}/release", s.handleReleaseAlert)
		r.Post("/alerts/{id}/revoke", s.handleRevokeAlert)
		r.Post("/alerts/{id}/close", s.handleCloseAlert)

		r.Get("/collectors", s.handleListCollectors)
		r.Get("/collectors/{id}", s.handleGetCollector)
		r.Post("/collectors", s.handleCreateCollector)
		r.Patch("/collectors/{id}", s.handleUpdateCollector)
		r.Post("/collectors/batch/disable", s.handleBatchDisable)

		r.Get("/rules", s.handleListRules)
		r.Post("/rules", s.handleCreateRule)
		r.Patch("/rules/{id}", s.handleUpdateRule)
		r.Delete("/rules/{id}", s.handleDeleteRule)
		r.Post("/rules/batch", s.handleBatchRules)

		r.Get("/audit", s.handleListAudit)
		r.Get("/assignments", s.handleListAssignments)
		r.Get("/backlog", s.handleBacklog)
		r.Get("/stats", s.handleStats)
		r.Get("/failures", s.handleListFailures)
		r.Post("/failures/{id}/reinject", s.handleReinjectFailure)
		r.Post("/failures/{id}/resolve", s.handleResolveFailure)
		r.Get("/shards", s.handleShardManifest)
	})

	r.Get("/", s.spaHandler)
	r.Get("/*", s.spaHandler)
	r.NotFound(s.spaHandler)
	return r
}

// spaHandler serves files from the frontend build directory, falling back to
// index.html for unknown paths so the Vue router can resolve them client-side.
func (s *Server) spaHandler(w http.ResponseWriter, r *http.Request) {
	dir := os.Getenv("TERMDUTY_FRONTEND_DIR")
	if dir == "" {
		dir = "web/dist"
	}
	requested := strings.TrimPrefix(r.URL.Path, "/")
	if requested == "" {
		requested = "index.html"
	}
	full := filepath.Join(dir, filepath.Clean("/"+requested))
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("frontend build not found; run `npm run build` in web/\n"))
}
