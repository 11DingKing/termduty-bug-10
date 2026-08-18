package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"termduty/internal/domain"
)

// Paged is the uniform paginated response envelope.
type Paged[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

func newPaged[T any](items []T, total int64, page domain.Page) Paged[T] {
	p := page.Normalize(1 << 31)
	pn := page.Offset/p.Size + 1
	return Paged[T]{Items: items, Total: total, Page: pn, PageSize: p.Size, HasMore: int64(page.Offset+p.Size) < total}
}

// parsePage extracts page and page_size from the query string. page is 1-based
// and the size is clamped to the configured maximum.
func parsePage(r *http.Request, defSize, maxSize int) (domain.Page, error) {
	pageStr := r.URL.Query().Get("page")
	sizeStr := r.URL.Query().Get("page_size")
	page := 1
	size := defSize
	var err error
	if pageStr != "" {
		if page, err = strconv.Atoi(pageStr); err != nil || page < 1 {
			return domain.Page{}, domain.ErrOutOfRange
		}
	}
	if sizeStr != "" {
		if size, err = strconv.Atoi(sizeStr); err != nil || size < 1 {
			return domain.Page{}, domain.ErrOutOfRange
		}
	}
	if size > maxSize {
		size = maxSize
	}
	return domain.Page{Offset: (page - 1) * size, Size: size}, nil
}

// parsePageOrFail parses pagination and writes the error response on failure so
// every list handler shares one validated entry path.
func (s *Server) parsePageOrFail(w http.ResponseWriter, r *http.Request) (domain.Page, bool) {
	page, err := parsePage(r, s.cfg.DefaultPageSize, s.cfg.MaxPageSize)
	if err != nil {
		writeErr(w, r, s.log, err)
		return domain.Page{}, false
	}
	return page, true
}

// writePaged wraps items in the paged envelope.
func writePaged[T any](w http.ResponseWriter, items []T, total int64, page domain.Page) {
	writeJSON(w, http.StatusOK, newPaged(items, total, page))
}

// AlertDTO keeps suppressed_until omitted until a suppression is in effect;
// time.Time has no zero-value omitempty so the DTO remains for this field.
type AlertDTO struct {
	ID              string `json:"id"`
	CollectorID     string `json:"collector_id"`
	RuleID          string `json:"rule_id"`
	ReadingID       string `json:"reading_id"`
	Severity        string `json:"severity"`
	State           string `json:"state"`
	Message         string `json:"message"`
	AssigneeID      string `json:"assignee_id"`
	FirstSeen       string `json:"first_seen"`
	LastSeen        string `json:"last_seen"`
	SuppressedUntil string `json:"suppressed_until,omitempty"`
	Version         int64  `json:"version"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func alertDTO(a domain.Alert) AlertDTO {
	sup := ""
	if !a.SuppressedUntil.IsZero() {
		sup = ts(a.SuppressedUntil)
	}
	return AlertDTO{
		ID: string(a.ID), CollectorID: string(a.CollectorID), RuleID: a.RuleID, ReadingID: string(a.ReadingID),
		Severity: string(a.Severity), State: string(a.State), Message: a.Message, AssigneeID: a.AssigneeID,
		FirstSeen: ts(a.FirstSeen), LastSeen: ts(a.LastSeen), SuppressedUntil: sup,
		Version: a.Version, CreatedAt: ts(a.CreatedAt), UpdatedAt: ts(a.UpdatedAt),
	}
}

func alertPage(items []domain.Alert) []AlertDTO {
	out := make([]AlertDTO, len(items))
	for i, a := range items {
		out[i] = alertDTO(a)
	}
	return out
}

// AssignmentDTO omits completed_at and note until they are set.
type AssignmentDTO struct {
	ID          string `json:"id"`
	AlertID     string `json:"alert_id"`
	HandlerID   string `json:"handler_id"`
	State       string `json:"state"`
	AcceptedAt  string `json:"accepted_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	Note        string `json:"note,omitempty"`
	Version     int64  `json:"version"`
}

func assignmentDTO(a domain.Assignment) AssignmentDTO {
	d := AssignmentDTO{
		ID: string(a.ID), AlertID: string(a.AlertID), HandlerID: a.HandlerID,
		State: string(a.State), AcceptedAt: ts(a.AcceptedAt), Note: a.Note, Version: a.Version,
	}
	if !a.CompletedAt.IsZero() {
		d.CompletedAt = ts(a.CompletedAt)
	}
	return d
}

func assignmentPage(items []domain.Assignment) []AssignmentDTO {
	out := make([]AssignmentDTO, len(items))
	for i, a := range items {
		out[i] = assignmentDTO(a)
	}
	return out
}

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
