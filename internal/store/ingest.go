package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"termduty/internal/domain"
)

const ingestCols = "id,collector_id,payload,status,leased_at,lease_expires_at,attempts,last_error,shard_id,line_no,created_at"

type ingestRepo struct{ *Store }

func (r *ingestRepo) Enqueue(ctx context.Context, item domain.IngestItem) error {
	if item.ID == "" {
		item.ID = domain.IngestID(newID())
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = r.Now()
	}
	if item.Status == "" {
		item.Status = domain.IngestPending
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO ingest_queue (id,collector_id,payload,status,attempts,last_error,created_at)
		VALUES (?,?,?,?,?,?,?)`,
		string(item.ID), string(item.CollectorID), string(item.Payload), string(item.Status),
		item.Attempts, item.LastError, timestamp(item.CreatedAt))
	return err
}

// LeaseBatch atomically claims up to limit pending or expired-lease items by
// granting each a fresh lease. The claim happens inside one immediate
// transaction so two workers cannot pick up the same item.
func (r *ingestRepo) LeaseBatch(ctx context.Context, now time.Time, ttl time.Duration, limit int) ([]domain.IngestItem, error) {
	if limit <= 0 {
		limit = 50
	}
	expiry := now.Add(ttl)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, "SELECT "+ingestCols+" FROM ingest_queue WHERE status = 'pending' OR (status = 'leased' AND lease_expires_at < ?) ORDER BY created_at ASC LIMIT ?",
		timestamp(now), limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.IngestItem, 0)
	for rows.Next() {
		it, err := scanIngest(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
	}
	rows.Close()
	for i := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE ingest_queue SET status = 'leased', leased_at = ?, lease_expires_at = ?, attempts = attempts + 1 WHERE id = ?`,
			timestamp(now), timestamp(expiry), string(items[i].ID)); err != nil {
			return nil, err
		}
		items[i].Status = domain.IngestLeased
		items[i].LeasedAt = now
		items[i].LeaseExpiresAt = expiry
		items[i].Attempts++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return items, nil
}

func (r *ingestRepo) Complete(ctx context.Context, id domain.IngestID, shardID string, lineNo int64) error {
	res, err := r.db.ExecContext(ctx, `UPDATE ingest_queue SET status = 'done', shard_id = ?, line_no = ? WHERE id = ? AND status = 'leased'`,
		shardID, lineNo, string(id))
	if err != nil {
		return err
	}
	return affectedOrConflict(res, "ingest", string(id), 0)
}

func (r *ingestRepo) Fail(ctx context.Context, id domain.IngestID, errMsg string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE ingest_queue SET status = 'failed', last_error = ? WHERE id = ?`, errMsg, string(id))
	return err
}

func (r *ingestRepo) ReclaimExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE ingest_queue SET status = 'pending', leased_at = NULL, lease_expires_at = NULL WHERE status = 'leased' AND lease_expires_at < ?`, timestamp(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *ingestRepo) PendingCount(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ingest_queue WHERE status = 'pending'`).Scan(&n)
	return n, err
}

func (r *ingestRepo) LeasedCount(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ingest_queue WHERE status = 'leased'`).Scan(&n)
	return n, err
}

func (r *ingestRepo) Get(ctx context.Context, id domain.IngestID) (domain.IngestItem, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+ingestCols+" FROM ingest_queue WHERE id = ?", string(id))
	it, err := scanIngest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IngestItem{}, &NotFoundError{Entity: "ingest", ID: string(id)}
	}
	return it, err
}

func scanIngest(sc rowScanner) (domain.IngestItem, error) {
	var it domain.IngestItem
	var id, collector, status, leased, expires, lastErr, shardID, created sql.NullString
	var payload []byte
	err := sc.Scan(&id, &collector, &payload, &status, &leased, &expires, &it.Attempts, &lastErr, &shardID, &it.LineNo, &created)
	if err != nil {
		return domain.IngestItem{}, err
	}
	it.ID = domain.IngestID(id.String)
	it.CollectorID = domain.CollectorID(collector.String)
	it.Payload = payload
	it.Status = domain.IngestStatus(status.String)
	it.LeasedAt = parseTS(leased)
	it.LeaseExpiresAt = parseTS(expires)
	it.LastError = lastErr.String
	it.ShardID = shardID.String
	it.CreatedAt = parseTS(created)
	return it, nil
}
