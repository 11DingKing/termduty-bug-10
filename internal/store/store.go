package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"termduty/internal/domain"
	"termduty/internal/store/shard"
)

// Store is the concrete persistence facade. It owns the embedded SQLite index
// (collectors, rules, alerts, assignments, audit, ingest queue, manifest and
// permanent failures) together with the sharded JSONL store that holds the
// authoritative reading records. Each aggregate is exposed through its own
// repository so the orchestration layer depends on narrow interfaces.
type Store struct {
	db     *sql.DB
	shards *shard.Store
	clock  domain.Clock
	log    *slog.Logger
}

// DB exposes the underlying connection for diagnostics and CLI commands.
func (s *Store) DB() *sql.DB { return s.db }

// Now returns the current time through the injected clock.
func (s *Store) Now() time.Time { return s.clock.Now() }

// Collectors returns the collector repository.
func (s *Store) Collectors() *collectorRepo { return &collectorRepo{s} }

// Rules returns the alert rule repository.
func (s *Store) Rules() *ruleRepo { return &ruleRepo{s} }

// Readings returns the reading repository backed by shards plus the index.
func (s *Store) Readings() *readingRepo { return &readingRepo{s} }

// Alerts returns the alert ticket repository.
func (s *Store) Alerts() *alertRepo { return &alertRepo{s} }

// Assignments returns the dispatch / acceptance repository.
func (s *Store) Assignments() *assignmentRepo { return &assignmentRepo{s} }

// Ingest returns the poll-pull ingest queue repository.
func (s *Store) Ingest() *ingestRepo { return &ingestRepo{s} }

// Audit returns the audit trail repository.
func (s *Store) Audit() AuditRepo { return &auditRepo{s} }

// Failures returns the permanent-failure (dead letter) repository.
func (s *Store) Failures() *failureRepo { return &failureRepo{s} }

// Ping verifies the database is reachable and the shard directory exists.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	return s.shards.CheckRoot(ctx)
}

// parseTS parses an RFC3339 timestamp stored as text; returns the zero value
// when the column was NULL or unparseable.
func parseTS(v sql.NullString) time.Time {
	if !v.Valid || v.String == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// newID generates a fresh identifier for records whose owner does not supply one.
func newID() string { return uuid.NewString() }

// NotFoundError carries the entity and key that could not be located. It wraps
// domain.ErrNotFound so callers can match it with errors.Is while still
// reporting which object was missing.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Entity, e.ID)
}

func (e *NotFoundError) Unwrap() error { return domain.ErrNotFound }

// ConflictError reports an optimistic-concurrency mismatch. The caller can
// retry by re-reading the current version.
type ConflictError struct {
	Entity  string
	ID      string
	Version int64
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %q version mismatch", e.Entity, e.ID)
}

func (e *ConflictError) Unwrap() error { return domain.ErrConflict }

// AlreadyAssignedError reports that an alert was accepted by another handler
// before the current request could take effect. It carries the winning handler
// so the loser can be told exactly who beat them to it.
type AlreadyAssignedError struct {
	AlertID    domain.AlertID
	AssigneeID string
}

func (e *AlreadyAssignedError) Error() string {
	return fmt.Sprintf("alert %s already accepted by %s", e.AlertID, e.AssigneeID)
}

func (e *AlreadyAssignedError) Unwrap() error { return domain.ErrAlreadyAssigned }

// buildWhere joins filter clauses with AND, defaulting to "1=1" when empty so
// every repository List method can share one code path.
func buildWhere(where []string) string {
	if len(where) == 0 {
		return "1=1"
	}
	return strings.Join(where, " AND ")
}

// eq appends a "<col> = ?" clause when val is non-empty, so every List filter
// shares one append path instead of repeating the three-line guard.
func eq(where *[]string, args *[]any, col, val string) {
	if val != "" {
		*where = append(*where, col+" = ?")
		*args = append(*args, val)
	}
}

// paginatedQuery runs a COUNT then a paginated SELECT, scanning each row with
// the provided scanner. It centralises the boilerplate shared by every
// repository List method so the offset and size boundary is validated once.
func paginatedQuery[T any](ctx context.Context, db *sql.DB, table, cols, orderBy, cond string, args []any, page domain.Page, scan func(rowScanner) (T, error)) ([]T, int64, error) {
	p := page.Normalize(200)
	var total int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := "SELECT " + cols + " FROM " + table + " WHERE " + cond + " ORDER BY " + orderBy + " LIMIT ? OFFSET ?"
	args = append(args, p.Size, p.Offset)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]T, 0, p.Size)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// affectedOrConflict reports the outcome of an optimistic update: a wrapped
// ConflictError when no row matched (wrong version) and nil on success. It
// centralises the RowsAffected boilerplate shared by every versioned write.
func affectedOrConflict(res sql.Result, entity, id string, version int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return &ConflictError{Entity: entity, ID: id, Version: version}
	}
	return nil
}
