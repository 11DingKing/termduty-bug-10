package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"termduty/internal/domain"
	"termduty/internal/orchestration"
	"termduty/internal/store"
)

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.RuleFilter{
		CollectorID: domain.CollectorID(r.URL.Query().Get("collector_id")),
		Metric:      domain.Metric(r.URL.Query().Get("metric")),
		Page:        page,
	}
	if v := r.URL.Query().Get("enabled"); v != "" {
		b := v == "true" || v == "1"
		f.Enabled = &b
	}
	items, total, err := s.store.Rules().List(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, items, total, page)
}

type ruleRequest struct {
	CollectorID   string   `json:"collector_id"`
	Metric        string   `json:"metric"`
	WindowSeconds int      `json:"window_seconds"`
	MinValue      *float64 `json:"min_value"`
	MaxValue      *float64 `json:"max_value"`
	FaultTrigger  string   `json:"fault_trigger"`
	Severity      string   `json:"severity"`
	Enabled       *bool    `json:"enabled"`
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	actor, ok := s.decodeRole(w, r, domain.RoleOps, &req)
	if !ok {
		return
	}
	created, err := s.svc.Admin.CreateRule(r.Context(), ruleFromRequest(req), actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	actor, ok := s.decodeRole(w, r, domain.RoleOps, &req)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cur, err := s.store.Rules().Get(r.Context(), id)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	applyRuleRequest(&cur, req)
	updated, err := s.svc.Admin.UpdateRule(r.Context(), cur, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.mustRole(w, r, domain.RoleOps)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil {
		writeErr(w, r, s.log, domain.ErrValidation)
		return
	}
	if err := s.svc.Admin.DeleteRule(r.Context(), id, version, actor); err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type batchRulesRequest struct {
	Updates []orchestration.RuleUpdate `json:"updates"`
}

func (s *Server) handleBatchRules(w http.ResponseWriter, r *http.Request) {
	var req batchRulesRequest
	actor, ok := s.decodeRole(w, r, domain.RoleOps, &req)
	if !ok {
		return
	}
	completed, err := s.svc.Batch.UpdateRules(r.Context(), req.Updates, actor)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"completed": completed, "error": err.Error(), "rolled_back": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": completed, "total": len(req.Updates)})
}

func ruleFromRequest(req ruleRequest) domain.Rule {
	ru := domain.Rule{
		Metric:        domain.Metric(req.Metric),
		WindowSeconds: req.WindowSeconds,
		MinValue:      req.MinValue,
		MaxValue:      req.MaxValue,
		FaultTrigger:  req.FaultTrigger,
		Severity:      domain.Severity(req.Severity),
		Enabled:       true,
	}
	if req.CollectorID != "" {
		c := domain.CollectorID(req.CollectorID)
		ru.CollectorID = &c
	}
	if req.Enabled != nil {
		ru.Enabled = *req.Enabled
	}
	return ru
}

func applyRuleRequest(ru *domain.Rule, req ruleRequest) {
	ru.Metric = domain.Metric(req.Metric)
	ru.WindowSeconds = req.WindowSeconds
	ru.MinValue = req.MinValue
	ru.MaxValue = req.MaxValue
	ru.FaultTrigger = req.FaultTrigger
	ru.Severity = domain.Severity(req.Severity)
	if req.Enabled != nil {
		ru.Enabled = *req.Enabled
	}
	if req.CollectorID != "" {
		c := domain.CollectorID(req.CollectorID)
		ru.CollectorID = &c
	}
}
