package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"termduty/internal/domain"
)

// PermanentFailure is a dead-letter record for background work that exhausted
// its retries. It is queryable and can be manually re-injected.
type PermanentFailure struct {
	ID        string    `json:"id"`
	TaskType  string    `json:"task_type"`
	TargetID  string    `json:"target_id"`
	Payload   string    `json:"payload"`
	LastError string    `json:"last_error"`
	Attempts  int       `json:"attempts"`
	Status    string    `json:"status"`
	FailedAt  time.Time `json:"failed_at"`
	Resolved  bool      `json:"resolved"`
}

// FailureFilter narrows the dead-letter listing.
type FailureFilter struct {
	TaskType string
	Status   string
	Resolved *bool
	Page     domain.Page
}

type failureRepo struct{ *Store }

func (r *failureRepo) Record(ctx context.Context, f PermanentFailure) error {
	if f.ID == "" {
		f.ID = newID()
	}
	if f.FailedAt.IsZero() {
		f.FailedAt = r.Now()
	}
	if f.Status == "" {
		f.Status = "dead"
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO permanent_failures (id,task_type,target_id,payload,last_error,attempts,status,failed_at,resolved)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		f.ID, f.TaskType, f.TargetID, f.Payload, f.LastError, f.Attempts, f.Status, timestamp(f.FailedAt), boolArg(f.Resolved))
	return err
}

func (r *failureRepo) Get(ctx context.Context, id string) (PermanentFailure, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,task_type,target_id,payload,last_error,attempts,status,failed_at,resolved FROM permanent_failures WHERE id = ?`, id)
	f, err := scanFailure(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PermanentFailure{}, &NotFoundError{Entity: "permanent_failure", ID: id}
	}
	return f, err
}

func (r *failureRepo) List(ctx context.Context, f FailureFilter) ([]PermanentFailure, int64, error) {
	var where []string
	var args []any
	eq(&where, &args, "task_type", f.TaskType)
	eq(&where, &args, "status", f.Status)
	if f.Resolved != nil {
		where = append(where, "resolved = ?")
		args = append(args, boolArg(*f.Resolved))
	}
	return paginatedQuery(ctx, r.db, "permanent_failures", "id,task_type,target_id,payload,last_error,attempts,status,failed_at,resolved", "failed_at DESC", buildWhere(where), args, f.Page, scanFailure)
}

func (r *failureRepo) Resolve(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE permanent_failures SET resolved = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return &NotFoundError{Entity: "permanent_failure", ID: id}
	}
	return nil
}

// Requeue marks a permanent failure resolved and resets the underlying work
// item so it can be picked up again. For ingest failures the queued reading is
// returned to pending; other task types are simply marked resolved.
func (r *failureRepo) Requeue(ctx context.Context, id string) (PermanentFailure, error) {
	f, err := r.Get(ctx, id)
	if err != nil {
		return PermanentFailure{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PermanentFailure{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE permanent_failures SET resolved = 1 WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return PermanentFailure{}, err
	}
	if f.TaskType == "ingest" && f.TargetID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE ingest_queue SET status = 'pending', leased_at = NULL, lease_expires_at = NULL WHERE id = ?`, f.TargetID); err != nil {
			tx.Rollback()
			return PermanentFailure{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PermanentFailure{}, err
	}
	f.Resolved = true
	return f, nil
}

func scanFailure(sc rowScanner) (PermanentFailure, error) {
	var f PermanentFailure
	var failedAt string
	var resolved int
	err := sc.Scan(&f.ID, &f.TaskType, &f.TargetID, &f.Payload, &f.LastError, &f.Attempts, &f.Status, &failedAt, &resolved)
	if err != nil {
		return PermanentFailure{}, err
	}
	f.Resolved = resolved == 1
	f.FailedAt = parseTS(sql.NullString{String: failedAt, Valid: failedAt != ""})
	return f, nil
}
