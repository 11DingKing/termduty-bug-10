package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"termduty/internal/domain"
)

// CollectorFilter narrows a collector listing.
type CollectorFilter struct {
	Kind   domain.CollectorKind
	Status domain.CollectorStatus
	Search string
	Page   domain.Page
}

type collectorRepo struct{ *Store }

func (r *collectorRepo) Create(ctx context.Context, c domain.Collector) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = r.Now()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
	if c.Version == 0 {
		c.Version = 1
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO collectors
		(id,code,name,kind,location,status,handler_id,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		string(c.ID), c.Code, c.Name, string(c.Kind), c.Location,
		string(c.Status), c.HandlerID, c.Version, timestamp(c.CreatedAt), timestamp(c.UpdatedAt))
	if err != nil && isUniqueViolation(err) {
		return fmt.Errorf("%w: collector code %s", domain.ErrAlreadyExists, c.Code)
	}
	return err
}

func (r *collectorRepo) Get(ctx context.Context, id domain.CollectorID) (domain.Collector, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,code,name,kind,location,status,handler_id,version,created_at,updated_at
		FROM collectors WHERE id = ?`, string(id))
	c, err := scanCollector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Collector{}, &NotFoundError{Entity: "collector", ID: string(id)}
	}
	return c, err
}

func (r *collectorRepo) List(ctx context.Context, f CollectorFilter) ([]domain.Collector, int64, error) {
	var where []string
	var args []any
	eq(&where, &args, "kind", string(f.Kind))
	eq(&where, &args, "status", string(f.Status))
	if f.Search != "" {
		where = append(where, "(name LIKE ? OR code LIKE ?)")
		args = append(args, "%"+f.Search+"%", "%"+f.Search+"%")
	}
	return paginatedQuery(ctx, r.db, "collectors", "id,code,name,kind,location,status,handler_id,version,created_at,updated_at", "created_at DESC", buildWhere(where), args, f.Page, scanCollector)
}

func (r *collectorRepo) Update(ctx context.Context, c *domain.Collector) error {
	if c == nil {
		return errors.New("nil collector")
	}
	c.UpdatedAt = r.Now()
	res, err := r.db.ExecContext(ctx, `UPDATE collectors SET code=?,name=?,kind=?,location=?,status=?,handler_id=?,version=version+1,updated_at=?
		WHERE id=? AND version=?`,
		c.Code, c.Name, string(c.Kind), c.Location, string(c.Status), c.HandlerID, timestamp(c.UpdatedAt),
		string(c.ID), c.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		exists, _ := r.Exists(ctx, c.ID)
		if !exists {
			return &NotFoundError{Entity: "collector", ID: string(c.ID)}
		}
		return &ConflictError{Entity: "collector", ID: string(c.ID), Version: c.Version}
	}
	c.Version++
	return nil
}

func (r *collectorRepo) SetStatus(ctx context.Context, id domain.CollectorID, to domain.CollectorStatus, expectVersion int64) (domain.Collector, error) {
	now := r.Now()
	res, err := r.db.ExecContext(ctx, `UPDATE collectors SET status=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		string(to), timestamp(now), string(id), expectVersion)
	if err != nil {
		return domain.Collector{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return domain.Collector{}, err
	}
	if n == 0 {
		exists, _ := r.Exists(ctx, id)
		if !exists {
			return domain.Collector{}, &NotFoundError{Entity: "collector", ID: string(id)}
		}
		return domain.Collector{}, &ConflictError{Entity: "collector", ID: string(id), Version: expectVersion}
	}
	return r.Get(ctx, id)
}

func (r *collectorRepo) Exists(ctx context.Context, id domain.CollectorID) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM collectors WHERE id = ?`, string(id)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCollector(sc rowScanner) (domain.Collector, error) {
	var c domain.Collector
	var id, kind, status, created, updated string
	err := sc.Scan(&id, &c.Code, &c.Name, &kind, &c.Location, &status, &c.HandlerID, &c.Version, &created, &updated)
	if err != nil {
		return domain.Collector{}, err
	}
	c.ID = domain.CollectorID(id)
	c.Kind = domain.CollectorKind(kind)
	c.Status = domain.CollectorStatus(status)
	c.CreatedAt = parseTS(sql.NullString{String: created, Valid: created != ""})
	c.UpdatedAt = parseTS(sql.NullString{String: updated, Valid: updated != ""})
	return c, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "constraint failed: UNIQUE")
}
