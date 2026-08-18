package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"termduty/internal/domain"
	"termduty/internal/store"
)

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.AuditFilter{
		Actor:      r.URL.Query().Get("actor"),
		Role:       domain.Role(r.URL.Query().Get("role")),
		TargetType: r.URL.Query().Get("target_type"),
		TargetID:   r.URL.Query().Get("target_id"),
		Page:       page,
	}
	items, total, err := s.store.Audit().List(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, items, total, page)
}

func (s *Server) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.AssignmentFilter{
		AlertID:   domain.AlertID(r.URL.Query().Get("alert_id")),
		HandlerID: r.URL.Query().Get("handler_id"),
		State:     domain.AssignmentState(r.URL.Query().Get("state")),
		Page:      page,
	}
	items, total, err := s.store.Assignments().List(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, assignmentPage(items), total, page)
}

func (s *Server) handleBacklog(w http.ResponseWriter, r *http.Request) {
	summary, err := s.svc.Alerts.Backlog(r.Context())
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"open_alerts":     summary.OpenAlerts,
		"assigned_alerts": summary.AssignedAlerts,
		"handling_alerts": summary.HandlingAlerts,
		"overdue_alerts":  alertPage(summary.OverdueAlerts),
		"pending_ingest":  summary.PendingIngest,
		"leased_ingest":   summary.LeasedIngest,
		"dead_lettered":   summary.DeadLettered,
		"failures":        summary.Failures,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.Query.Stats(r.Context())
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleListFailures(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.FailureFilter{
		TaskType: r.URL.Query().Get("task_type"),
		Status:   r.URL.Query().Get("status"),
		Page:     page,
	}
	if v := r.URL.Query().Get("resolved"); v != "" {
		b := v == "true" || v == "1"
		f.Resolved = &b
	}
	items, total, err := s.svc.Batch.ListFailures(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, items, total, page)
}

func (s *Server) handleReinjectFailure(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.mustRole(w, r, domain.RoleOps)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	f, err := s.svc.Batch.ReinjectFailure(r.Context(), id, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleResolveFailure(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.mustRole(w, r, domain.RoleOps)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.svc.Batch.ResolveFailure(r.Context(), id, actor); err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleShardManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := s.store.Readings().Manifest(r.Context())
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shards": manifest, "total": strconv.Itoa(len(manifest))})
}
