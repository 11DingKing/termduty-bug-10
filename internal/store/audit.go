package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"termduty/internal/domain"
)

// AuditFilter narrows the audit trail query.
type AuditFilter struct {
	Actor      string
	Role       domain.Role
	TargetType string
	TargetID   string
	Page       domain.Page
}

// AuditRepo persists immutable audit entries.
type AuditRepo interface {
	Append(ctx context.Context, e domain.AuditEntry) error
	List(ctx context.Context, f AuditFilter) ([]domain.AuditEntry, int64, error)
}

type auditRepo struct{ *Store }

func (r *auditRepo) Append(ctx context.Context, e domain.AuditEntry) error {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.At.IsZero() {
		e.At = r.Now()
	}
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO audit (id,actor,role,action,target_type,target_id,detail,at) VALUES (?,?,?,?,?,?,?,?)`,
		e.ID, e.Actor, string(e.Role), e.Action, e.TargetType, e.TargetID, string(detail), timestamp(e.At))
	return err
}

func (r *auditRepo) List(ctx context.Context, f AuditFilter) ([]domain.AuditEntry, int64, error) {
	var where []string
	var args []any
	eq(&where, &args, "actor", f.Actor)
	eq(&where, &args, "role", string(f.Role))
	eq(&where, &args, "target_type", f.TargetType)
	eq(&where, &args, "target_id", f.TargetID)
	cond := buildWhere(where)
	return paginatedQuery(ctx, r.db, "audit", "id,actor,role,action,target_type,target_id,detail,at", "at DESC", cond, args, f.Page, scanAudit)
}

func scanAudit(sc rowScanner) (domain.AuditEntry, error) {
	var e domain.AuditEntry
	var role, detail, at string
	err := sc.Scan(&e.ID, &e.Actor, &role, &e.Action, &e.TargetType, &e.TargetID, &detail, &at)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	e.Role = domain.Role(role)
	e.At = parseTS(sql.NullString{String: at, Valid: at != ""})
	if detail != "" {
		_ = json.Unmarshal([]byte(detail), &e.Detail)
	}
	return e, nil
}
