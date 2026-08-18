package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"termduty/internal/domain"
	"termduty/internal/store"
)

func (s *Server) handleListCollectors(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.CollectorFilter{
		Kind:   domain.CollectorKind(r.URL.Query().Get("kind")),
		Status: domain.CollectorStatus(r.URL.Query().Get("status")),
		Search: r.URL.Query().Get("q"),
		Page:   page,
	}
	items, total, err := s.store.Collectors().List(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, items, total, page)
}

type collectorRequest struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Location  string `json:"location"`
	Status    string `json:"status"`
	HandlerID string `json:"handler_id"`
}

func (s *Server) handleCreateCollector(w http.ResponseWriter, r *http.Request) {
	var req collectorRequest
	actor, ok := s.decodeRole(w, r, domain.RoleOps, &req)
	if !ok {
		return
	}
	c := domain.Collector{
		Code: req.Code, Name: req.Name, Kind: domain.CollectorKind(req.Kind),
		Location: req.Location, Status: domain.CollectorStatus(req.Status), HandlerID: req.HandlerID,
	}
	created, err := s.svc.Admin.CreateCollector(r.Context(), c, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateCollector(w http.ResponseWriter, r *http.Request) {
	var req collectorRequest
	actor, ok := s.decodeRole(w, r, domain.RoleOps, &req)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cur, err := s.store.Collectors().Get(r.Context(), domain.CollectorID(id))
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	cur.Code = req.Code
	cur.Name = req.Name
	cur.Kind = domain.CollectorKind(req.Kind)
	cur.Location = req.Location
	cur.Status = domain.CollectorStatus(req.Status)
	cur.HandlerID = req.HandlerID
	updated, err := s.svc.Admin.UpdateCollector(r.Context(), cur, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

type batchDisableRequest struct {
	IDs []string `json:"ids"`
}

func (s *Server) handleBatchDisable(w http.ResponseWriter, r *http.Request) {
	var req batchDisableRequest
	actor, ok := s.decodeRole(w, r, domain.RoleOps, &req)
	if !ok {
		return
	}
	ids := make([]domain.CollectorID, 0, len(req.IDs))
	for _, id := range req.IDs {
		ids = append(ids, domain.CollectorID(id))
	}
	completed, err := s.svc.Batch.DisableCollectors(r.Context(), ids, actor)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"completed": completed, "error": err.Error(), "rolled_back": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": completed, "total": len(ids)})
}

func (s *Server) handleGetCollector(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := s.store.Collectors().Get(r.Context(), domain.CollectorID(id))
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
