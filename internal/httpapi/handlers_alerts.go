package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"termduty/internal/domain"
	"termduty/internal/orchestration"
	"termduty/internal/store"
)

type handlerActionRequest struct {
	HandlerID string `json:"handler_id"`
	Note      string `json:"note"`
}

// alertActionInput resolves the shared inputs for a handler-driven alert
// transition: the actor must be a handler, the alert id comes from the path, and
// an optional handler_id defaults to the acting handler so every transition is
// attributed.
func (s *Server) alertActionInput(w http.ResponseWriter, r *http.Request) (orchestration.Actor, domain.AlertID, handlerActionRequest, bool) {
	actor, ok := s.mustRole(w, r, domain.RoleHandler)
	if !ok {
		return orchestration.Actor{}, "", handlerActionRequest{}, false
	}
	var req handlerActionRequest
	_ = decodeJSON(r, &req)
	if req.HandlerID == "" {
		req.HandlerID = actor.ID
	}
	return actor, domain.AlertID(chi.URLParam(r, "id")), req, true
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.AlertFilter{
		CollectorID: domain.CollectorID(r.URL.Query().Get("collector_id")),
		State:       domain.AlertState(r.URL.Query().Get("state")),
		Severity:    domain.Severity(r.URL.Query().Get("severity")),
		Page:        page,
	}
	f.From, _ = parseTimeQuery(r, "from")
	f.To, _ = parseTimeQuery(r, "to")
	items, total, err := s.svc.Alerts.List(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, alertPage(items), total, page)
}

func (s *Server) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := s.svc.Alerts.Get(r.Context(), domain.AlertID(id))
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, alertDTO(a))
}

func (s *Server) handleAcceptAlert(w http.ResponseWriter, r *http.Request) {
	actor, id, req, ok := s.alertActionInput(w, r)
	if !ok {
		return
	}
	assignment, alert, err := s.svc.Alerts.Accept(r.Context(), id, req.HandlerID, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignment": assignmentDTO(assignment), "alert": alertDTO(alert)})
}

func (s *Server) handleStartAlert(w http.ResponseWriter, r *http.Request) {
	actor, id, req, ok := s.alertActionInput(w, r)
	if !ok {
		return
	}
	alert, err := s.svc.Alerts.Start(r.Context(), id, req.HandlerID, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, alertDTO(alert))
}

func (s *Server) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	actor, id, req, ok := s.alertActionInput(w, r)
	if !ok {
		return
	}
	alert, err := s.svc.Alerts.Resolve(r.Context(), id, req.HandlerID, req.Note, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, alertDTO(alert))
}

func (s *Server) handleReleaseAlert(w http.ResponseWriter, r *http.Request) {
	actor, id, req, ok := s.alertActionInput(w, r)
	if !ok {
		return
	}
	alert, err := s.svc.Alerts.Release(r.Context(), id, req.HandlerID, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, alertDTO(alert))
}

func (s *Server) handleRevokeAlert(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.mustRole(w, r, domain.RoleDuty)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	alert, err := s.svc.Alerts.Revoke(r.Context(), domain.AlertID(id), actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, alertDTO(alert))
}

func (s *Server) handleCloseAlert(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.mustRole(w, r, domain.RoleDuty)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	alert, err := s.svc.Alerts.Close(r.Context(), domain.AlertID(id), actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, alertDTO(alert))
}
