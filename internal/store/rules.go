package store

import (
	"context"
	"database/sql"
	"errors"

	"termduty/internal/domain"
)

// RuleFilter narrows a rule listing.
type RuleFilter struct {
	CollectorID domain.CollectorID
	Metric      domain.Metric
	Enabled     *bool
	Page        domain.Page
}

type ruleRepo struct{ *Store }

func (r *ruleRepo) Create(ctx context.Context, ru domain.Rule) error {
	if ru.CreatedAt.IsZero() {
		ru.CreatedAt = r.Now()
	}
	if ru.UpdatedAt.IsZero() {
		ru.UpdatedAt = ru.CreatedAt
	}
	if ru.Version == 0 {
		ru.Version = 1
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO rules
		(id,collector_id,metric,window_seconds,min_value,max_value,fault_trigger,severity,enabled,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		ru.ID, collectorArg(ru.CollectorID), string(ru.Metric), ru.WindowSeconds,
		floatArg(ru.MinValue), floatArg(ru.MaxValue), ru.FaultTrigger, string(ru.Severity),
		boolArg(ru.Enabled), ru.Version, timestamp(ru.CreatedAt), timestamp(ru.UpdatedAt))
	return err
}

func (r *ruleRepo) Get(ctx context.Context, id string) (domain.Rule, error) {
	row := r.db.QueryRowContext(ctx, ruleSelectCols+` FROM rules WHERE id = ?`, id)
	ru, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Rule{}, &NotFoundError{Entity: "rule", ID: id}
	}
	return ru, err
}

func (r *ruleRepo) List(ctx context.Context, f RuleFilter) ([]domain.Rule, int64, error) {
	var where []string
	var args []any
	eq(&where, &args, "collector_id", string(f.CollectorID))
	eq(&where, &args, "metric", string(f.Metric))
	if f.Enabled != nil {
		where = append(where, "enabled = ?")
		args = append(args, boolArg(*f.Enabled))
	}
	return paginatedQuery(ctx, r.db, "rules", "id,collector_id,metric,window_seconds,min_value,max_value,fault_trigger,severity,enabled,version,created_at,updated_at", "created_at DESC", buildWhere(where), args, f.Page, scanRule)
}

func (r *ruleRepo) Update(ctx context.Context, ru *domain.Rule) error {
	if ru == nil {
		return errors.New("nil rule")
	}
	ru.UpdatedAt = r.Now()
	res, err := r.db.ExecContext(ctx, `UPDATE rules SET collector_id=?,metric=?,window_seconds=?,min_value=?,max_value=?,fault_trigger=?,severity=?,enabled=?,version=version+1,updated_at=?
		WHERE id=? AND version=?`,
		collectorArg(ru.CollectorID), string(ru.Metric), ru.WindowSeconds,
		floatArg(ru.MinValue), floatArg(ru.MaxValue), ru.FaultTrigger, string(ru.Severity),
		boolArg(ru.Enabled), timestamp(ru.UpdatedAt), ru.ID, ru.Version)
	if err != nil {
		return err
	}
	if err := affectedOrConflict(res, "rule", ru.ID, ru.Version); err != nil {
		return err
	}
	ru.Version++
	return nil
}

func (r *ruleRepo) Delete(ctx context.Context, id string, expectVersion int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM rules WHERE id=? AND version=?`, id, expectVersion)
	if err != nil {
		return err
	}
	return affectedOrConflict(res, "rule", id, expectVersion)
}

func (r *ruleRepo) EnabledFor(ctx context.Context, id domain.CollectorID) ([]domain.Rule, error) {
	rows, err := r.db.QueryContext(ctx, ruleSelectCols+` FROM rules WHERE enabled = 1 AND (collector_id IS NULL OR collector_id = ?) ORDER BY created_at ASC`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Rule, 0)
	for rows.Next() {
		ru, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ru)
	}
	return out, rows.Err()
}

const ruleSelectCols = `SELECT id,collector_id,metric,window_seconds,min_value,max_value,fault_trigger,severity,enabled,version,created_at,updated_at`

func scanRule(sc rowScanner) (domain.Rule, error) {
	var ru domain.Rule
	var collectorID sql.NullString
	var metric, severity, fault, created, updated string
	var minVal, maxVal sql.NullFloat64
	var enabled int
	err := sc.Scan(&ru.ID, &collectorID, &metric, &ru.WindowSeconds, &minVal, &maxVal, &fault, &severity, &enabled, &ru.Version, &created, &updated)
	if err != nil {
		return domain.Rule{}, err
	}
	if collectorID.Valid && collectorID.String != "" {
		c := domain.CollectorID(collectorID.String)
		ru.CollectorID = &c
	}
	ru.Metric = domain.Metric(metric)
	ru.Severity = domain.Severity(severity)
	ru.FaultTrigger = fault
	ru.Enabled = enabled == 1
	if minVal.Valid {
		v := minVal.Float64
		ru.MinValue = &v
	}
	if maxVal.Valid {
		v := maxVal.Float64
		ru.MaxValue = &v
	}
	ru.CreatedAt = parseTS(sql.NullString{String: created, Valid: created != ""})
	ru.UpdatedAt = parseTS(sql.NullString{String: updated, Valid: updated != ""})
	return ru, nil
}

func collectorArg(c *domain.CollectorID) any {
	if c == nil || *c == "" {
		return nil
	}
	return string(*c)
}

func floatArg(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolArg(b bool) int {
	if b {
		return 1
	}
	return 0
}
