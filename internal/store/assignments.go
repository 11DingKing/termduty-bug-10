package store

import (
	"context"
	"database/sql"
	"errors"

	"termduty/internal/domain"
)

// AssignmentFilter narrows a dispatch listing.
type AssignmentFilter struct {
	AlertID   domain.AlertID
	HandlerID string
	State     domain.AssignmentState
	Page      domain.Page
}

type assignmentRepo struct{ *Store }

func (r *assignmentRepo) Create(ctx context.Context, a domain.Assignment) error {
	if a.ID == "" {
		a.ID = domain.AssignmentID(newID())
	}
	if a.AcceptedAt.IsZero() {
		a.AcceptedAt = r.Now()
	}
	if a.Version == 0 {
		a.Version = 1
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO assignments (id,alert_id,handler_id,state,accepted_at,completed_at,note,version)
		VALUES (?,?,?,?,?,?,?,?)`,
		string(a.ID), string(a.AlertID), a.HandlerID, string(a.State), timestamp(a.AcceptedAt),
		suppressArg(a.CompletedAt), a.Note, a.Version)
	if err != nil && isUniqueViolation(err) {
		return &AlreadyAssignedError{AlertID: a.AlertID}
	}
	return err
}

func (r *assignmentRepo) Get(ctx context.Context, id domain.AssignmentID) (domain.Assignment, error) {
	row := r.db.QueryRowContext(ctx, assignmentSelectCols+` FROM assignments WHERE id = ?`, string(id))
	a, err := scanAssignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Assignment{}, &NotFoundError{Entity: "assignment", ID: string(id)}
	}
	return a, err
}

func (r *assignmentRepo) ActiveFor(ctx context.Context, alertID domain.AlertID) (domain.Assignment, bool, error) {
	row := r.db.QueryRowContext(ctx, assignmentSelectCols+` FROM assignments WHERE alert_id = ? AND state = 'active' LIMIT 1`, string(alertID))
	a, err := scanAssignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Assignment{}, false, nil
	}
	if err != nil {
		return domain.Assignment{}, false, err
	}
	return a, true, nil
}

func (r *assignmentRepo) List(ctx context.Context, f AssignmentFilter) ([]domain.Assignment, int64, error) {
	var where []string
	var args []any
	eq(&where, &args, "alert_id", string(f.AlertID))
	eq(&where, &args, "handler_id", f.HandlerID)
	eq(&where, &args, "state", string(f.State))
	return paginatedQuery(ctx, r.db, "assignments", "id,alert_id,handler_id,state,accepted_at,completed_at,note,version", "accepted_at DESC", buildWhere(where), args, f.Page, scanAssignment)
}

func (r *assignmentRepo) Complete(ctx context.Context, id domain.AssignmentID, note string, expectVersion int64) (domain.Assignment, error) {
	now := r.Now()
	res, err := r.db.ExecContext(ctx, `UPDATE assignments SET state = 'completed', completed_at = ?, note = ?, version = version+1 WHERE id = ? AND version = ? AND state = 'active'`,
		timestamp(now), note, string(id), expectVersion)
	if err != nil {
		return domain.Assignment{}, err
	}
	if err := affectedOrConflict(res, "assignment", string(id), expectVersion); err != nil {
		return domain.Assignment{}, err
	}
	return r.Get(ctx, id)
}

// AcceptAlert claims an alert for a handler. Because the transaction is opened
// in immediate mode the claim is serialised across concurrent accepters: the
// first commit wins, and every later caller observes the winning state and
// receives an AlreadyAssignedError naming the handler that took the ticket.
func (r *assignmentRepo) AcceptAlert(ctx context.Context, alertID domain.AlertID, handlerID string) (domain.Assignment, domain.Alert, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Assignment{}, domain.Alert{}, err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	var state, curAssignee string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT state, assignee_id, version FROM alerts WHERE id = ?`, string(alertID)).Scan(&state, &curAssignee, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Assignment{}, domain.Alert{}, &NotFoundError{Entity: "alert", ID: string(alertID)}
	}
	if err != nil {
		return domain.Assignment{}, domain.Alert{}, err
	}
	if state != string(domain.AlertStateOpen) {
		winner := curAssignee
		if state == string(domain.AlertStateResolved) || state == string(domain.AlertStateClosed) || state == string(domain.AlertStateRevoked) {
			winner = ""
		}
		return domain.Assignment{}, domain.Alert{}, &AlreadyAssignedError{AlertID: alertID, AssigneeID: winner}
	}
	now := r.Now()
	assignment := domain.Assignment{
		ID:         domain.AssignmentID(newID()),
		AlertID:    alertID,
		HandlerID:  handlerID,
		State:      domain.AssignmentActive,
		AcceptedAt: now,
		Version:    1,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO assignments (id,alert_id,handler_id,state,accepted_at,version) VALUES (?,?,?,?,?,?)`,
		string(assignment.ID), string(assignment.AlertID), assignment.HandlerID, string(assignment.State), timestamp(now), assignment.Version); err != nil {
		if isUniqueViolation(err) {
			return domain.Assignment{}, domain.Alert{}, &AlreadyAssignedError{AlertID: alertID, AssigneeID: curAssignee}
		}
		return domain.Assignment{}, domain.Alert{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE alerts SET state = 'assigned', assignee_id = ?, version = version+1, updated_at = ?, last_seen = ? WHERE id = ? AND version = ?`,
		handlerID, timestamp(now), timestamp(now), string(alertID), version)
	if err != nil {
		return domain.Assignment{}, domain.Alert{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return domain.Assignment{}, domain.Alert{}, err
	}
	if n == 0 {
		return domain.Assignment{}, domain.Alert{}, &ConflictError{Entity: "alert", ID: string(alertID), Version: version}
	}
	if err := tx.Commit(); err != nil {
		return domain.Assignment{}, domain.Alert{}, err
	}
	committed = true
	alert, err := r.Alerts().Get(ctx, alertID)
	if err != nil {
		return assignment, domain.Alert{}, err
	}
	return assignment, alert, nil
}

const assignmentSelectCols = `SELECT id,alert_id,handler_id,state,accepted_at,completed_at,note,version`

func scanAssignment(sc rowScanner) (domain.Assignment, error) {
	var a domain.Assignment
	var id, alert, handler, state, accepted, completed sql.NullString
	err := sc.Scan(&id, &alert, &handler, &state, &accepted, &completed, &a.Note, &a.Version)
	if err != nil {
		return domain.Assignment{}, err
	}
	a.ID = domain.AssignmentID(id.String)
	a.AlertID = domain.AlertID(alert.String)
	a.HandlerID = handler.String
	a.State = domain.AssignmentState(state.String)
	a.AcceptedAt = parseTS(accepted)
	a.CompletedAt = parseTS(completed)
	return a, nil
}
