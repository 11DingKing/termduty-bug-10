package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"termduty/internal/domain"
	"termduty/internal/store"
)

type ingestRequest struct {
	CollectorID string             `json:"collector_id"`
	Timestamp   string             `json:"timestamp"`
	QueueCount  int                `json:"queue_count"`
	DurationMs  int64              `json:"duration_ms"`
	FaultCode   string             `json:"fault_code"`
	RawMetrics  map[string]float64 `json:"raw_metrics"`
	Source      string             `json:"source"`
	Seq         int64              `json:"seq"`
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var req ingestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, r, s.log, domain.ErrValidation)
		return
	}
	var ts time.Time
	if req.Timestamp != "" {
		parsed, err := time.Parse(time.RFC3339Nano, req.Timestamp)
		if err != nil {
			writeErr(w, r, s.log, domain.ErrValidation)
			return
		}
		ts = parsed
	}
	sub := domain.ReadingSubmission{
		CollectorID: domain.CollectorID(req.CollectorID),
		Timestamp:   ts,
		QueueCount:  req.QueueCount,
		DurationMs:  req.DurationMs,
		FaultCode:   req.FaultCode,
		RawMetrics:  req.RawMetrics,
		Source:      req.Source,
		Seq:         req.Seq,
	}
	actor := actorFromContext(r.Context())
	id, err := s.svc.Ingest.Enqueue(r.Context(), sub, actor)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": string(id), "status": "queued"})
}

func (s *Server) handleListReadings(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.ReadingFilter{
		CollectorID: domain.CollectorID(r.URL.Query().Get("collector_id")),
		FaultCode:   r.URL.Query().Get("fault_code"),
		Page:        page,
	}
	f.From, _ = parseTimeQuery(r, "from")
	f.To, _ = parseTimeQuery(r, "to")
	items, total, err := s.store.Readings().List(r.Context(), f)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writePaged(w, items, total, page)
}

func (s *Server) handleGetReading(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rd, err := s.store.Readings().Get(r.Context(), domain.ReadingID(id))
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, rd)
}

func (s *Server) handleExportReadings(w http.ResponseWriter, r *http.Request) {
	page, ok := s.parsePageOrFail(w, r)
	if !ok {
		return
	}
	f := store.ReadingFilter{
		CollectorID: domain.CollectorID(r.URL.Query().Get("collector_id")),
		FaultCode:   r.URL.Query().Get("fault_code"),
		Page:        page,
	}
	f.From, _ = parseTimeQuery(r, "from")
	f.To, _ = parseTimeQuery(r, "to")
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="readings.jsonl"`)
	w.WriteHeader(http.StatusOK)
	count, err := s.svc.Query.ExportReadings(r.Context(), f, w)
	if err != nil {
		writeErr(w, r, s.log, err)
		return
	}
	w.Header().Set("X-Exported-Count", strconv.Itoa(count))
}

func parseTimeQuery(r *http.Request, key string) (time.Time, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
