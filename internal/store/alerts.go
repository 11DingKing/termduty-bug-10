package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"termduty/internal/domain"
)

// AlertFilter narrows an alert listing.
type AlertFilter struct {
	CollectorID domain.CollectorID
	State       domain.AlertState
	Severity    domain.Severity
	From        time.Time
	To          time.Time
	Page        domain.Page
}

type alertRepo struct{ *Store }

func (r *alertRepo) Create(ctx context.Context, a domain.Alert) error {
	if a.ID == "" {
		a.ID = domain.AlertID(newID())
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = r.Now()
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = a.CreatedAt
	}
	if a.FirstSeen.IsZero() {
		a.FirstSeen = a.CreatedAt
	}
	if a.LastSeen.IsZero() {
		a.LastSeen = a.FirstSeen
	}
	if a.Version == 0 {
		a.Version = 1
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO alerts
		(id,collector_id,rule_id,reading_id,severity,state,message,assignee_id,first_seen,last_seen,suppressed_until,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(a.ID), string(a.CollectorID), a.RuleID, string(a.ReadingID), string(a.Severity),
		string(a.State), a.Message, a.AssigneeID, timestamp(a.FirstSeen), timestamp(a.LastSeen),
		suppressArg(a.SuppressedUntil), a.Version, timestamp(a.CreatedAt), timestamp(a.UpdatedAt))
	return err
}

func (r *alertRepo) Get(ctx context.Context, id domain.AlertID) (domain.Alert, error) {
	row := r.db.QueryRowContext(ctx, alertSelectCols+` FROM alerts WHERE id = ?`, string(id))
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Alert{}, &NotFoundError{Entity: "alert", ID: string(id)}
	}
	return a, err
}

func (r *alertRepo) List(ctx context.Context, f AlertFilter) ([]domain.Alert, int64, error) {
	where, args := alertWhere(f)
	return paginatedQuery(ctx, r.db, "alerts", "id,collector_id,rule_id,reading_id,severity,state,message,assignee_id,first_seen,last_seen,suppressed_until,version,created_at,updated_at", "created_at DESC", where, args, f.Page, scanAlert)
}

func (r *alertRepo) Update(ctx context.Context, a *domain.Alert) error {
	if a == nil {
		return errors.New("nil alert")
	}
	a.UpdatedAt = r.Now()
	res, err := r.db.ExecContext(ctx, `UPDATE alerts SET state=?,message=?,assignee_id=?,last_seen=?,suppressed_until=?,severity=?,version=version+1,updated_at=?
		WHERE id=? AND version=?`,
		string(a.State), a.Message, a.AssigneeID, timestamp(a.LastSeen), suppressArg(a.SuppressedUntil),
		string(a.Severity), timestamp(a.UpdatedAt), string(a.ID), a.Version)
	if err != nil {
		return err
	}
	if err := affectedOrConflict(res, "alert", string(a.ID), a.Version); err != nil {
		return err
	}
	a.Version++
	return nil
}

func (r *alertRepo) FindActive(ctx context.Context, collectorID domain.CollectorID, ruleID string) (domain.Alert, bool, error) {
	row := r.db.QueryRowContext(ctx, alertSelectCols+` FROM alerts
		WHERE collector_id = ? AND rule_id = ? AND state IN ('open','assigned','handling')
		ORDER BY created_at DESC LIMIT 1`, string(collectorID), ruleID)
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Alert{}, false, nil
	}
	if err != nil {
		return domain.Alert{}, false, err
	}
	return a, true, nil
}

func (r *alertRepo) CountByState(ctx context.Context) (map[domain.AlertState]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM alerts GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[domain.AlertState]int64)
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[domain.AlertState(state)] = n
	}
	return out, rows.Err()
}

const alertSelectCols = `SELECT id,collector_id,rule_id,reading_id,severity,state,message,assignee_id,first_seen,last_seen,suppressed_until,version,created_at,updated_at`

func scanAlert(sc rowScanner) (domain.Alert, error) {
	var a domain.Alert
	var id, collector, rule, reading, severity, state, first, last, suppressed, created, updated string
	err := sc.Scan(&id, &collector, &rule, &reading, &severity, &state, &a.Message, &a.AssigneeID,
		&first, &last, &suppressed, &a.Version, &created, &updated)
	if err != nil {
		return domain.Alert{}, err
	}
	a.ID = domain.AlertID(id)
	a.CollectorID = domain.CollectorID(collector)
	a.RuleID = rule
	a.ReadingID = domain.ReadingID(reading)
	a.Severity = domain.Severity(severity)
	a.State = domain.AlertState(state)
	a.FirstSeen = parseTS(sql.NullString{String: first, Valid: first != ""})
	a.LastSeen = parseTS(sql.NullString{String: last, Valid: last != ""})
	a.SuppressedUntil = parseTS(sql.NullString{String: suppressed, Valid: suppressed != ""})
	a.CreatedAt = parseTS(sql.NullString{String: created, Valid: created != ""})
	a.UpdatedAt = parseTS(sql.NullString{String: updated, Valid: updated != ""})
	return a, nil
}

func alertWhere(f AlertFilter) (string, []any) {
	var where []string
	var args []any
	eq(&where, &args, "collector_id", string(f.CollectorID))
	eq(&where, &args, "state", string(f.State))
	eq(&where, &args, "severity", string(f.Severity))
	if !f.From.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, timestamp(f.From))
	}
	if !f.To.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, timestamp(f.To))
	}
	return buildWhere(where), args
}

func suppressArg(t time.Time) any {
	if t.IsZero() {
		return ""
	}
	return timestamp(t)
}

// transaction. It validates the move against the explicit state table, performs
// any assignment side-effects (completing or cancelling the active dispatch),
// and only commits when every step succeeds. Callers receive the resulting
// alert or a wrapped domain error describing why the move was rejected.
func (r *alertRepo) Transition(ctx context.Context, alertID domain.AlertID, event domain.AlertEvent, handlerID string, note string) (domain.Alert, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Alert{}, err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	var id, state, assignee string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT id, state, assignee_id, version FROM alerts WHERE id = ?`, string(alertID)).Scan(&id, &state, &assignee, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Alert{}, &NotFoundError{Entity: "alert", ID: string(alertID)}
	}
	if err != nil {
		return domain.Alert{}, err
	}
	next, err := domain.NextState(domain.AlertState(state), event)
	if err != nil {
		return domain.Alert{}, err
	}
	now := r.Now()
	newAssignee := assignee
	switch event {
	case domain.EventResolve:
		var aHandler string
		err := tx.QueryRowContext(ctx, `SELECT handler_id FROM assignments WHERE alert_id = ? AND state = 'active'`, string(alertID)).Scan(&aHandler)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Alert{}, fmt.Errorf("%w: alert %s has no active assignment", domain.ErrValidation, alertID)
		}
		if err != nil {
			return domain.Alert{}, err
		}
		if handlerID != "" && aHandler != handlerID {
			return domain.Alert{}, fmt.Errorf("%w: handler %s is not the active assignee of %s", domain.ErrValidation, handlerID, alertID)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE assignments SET state = 'completed', completed_at = ?, note = ?, version = version+1 WHERE alert_id = ? AND state = 'active'`,
			timestamp(now), note, string(alertID)); err != nil {
			return domain.Alert{}, err
		}
	case domain.EventRevoke, domain.EventRelease:
		if _, err := tx.ExecContext(ctx, `UPDATE assignments SET state = 'cancelled', version = version+1 WHERE alert_id = ? AND state = 'active'`, string(alertID)); err != nil {
			return domain.Alert{}, err
		}
		newAssignee = ""
	}
	if _, err := tx.ExecContext(ctx, `UPDATE alerts SET state = ?, assignee_id = ?, last_seen = ?, updated_at = ?, version = version+1 WHERE id = ?`,
		string(next), newAssignee, timestamp(now), timestamp(now), string(alertID)); err != nil {
		return domain.Alert{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Alert{}, err
	}
	committed = true
	return r.Get(ctx, alertID)
}
