package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"termduty/internal/domain"
	"termduty/internal/store/shard"
)

// ReadingFilter narrows a reading listing or export.
type ReadingFilter struct {
	CollectorID domain.CollectorID
	From        time.Time
	To          time.Time
	FaultCode   string
	Page        domain.Page
}

// ShardManifest is one row of the reading shard manifest index.
type ShardManifest struct {
	ShardID   string    `json:"shard_id"`
	Bucket    string    `json:"bucket"`
	Path      string    `json:"path"`
	FirstTS   time.Time `json:"first_ts"`
	LastTS    time.Time `json:"last_ts"`
	Count     int64     `json:"count"`
	Checksum  string    `json:"checksum"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ShardVerifyResult is the outcome of verifying one shard against its manifest.
type ShardVerifyResult struct {
	ShardID          string
	Count            int64
	ManifestChecksum string
	ActualChecksum   string
	OK               bool
	Error            string
}

// RebuildResult summarises an index rebuild.
type RebuildResult struct {
	Shards  int
	Rows    int64
	Skipped []string
}

type readingRepo struct{ *Store }

func (r *readingRepo) Append(ctx context.Context, rd domain.Reading) (domain.Reading, error) {
	if rd.ID == "" {
		rd.ID = domain.ReadingID(newID())
	}
	if rd.Timestamp.IsZero() {
		rd.Timestamp = r.Now()
	}
	if rd.IngestedAt.IsZero() {
		rd.IngestedAt = r.Now()
	}
	shardID := shard.ShardID("readings", rd.Timestamp.UTC().Format("2006-01-02"))
	lease, err := r.shards.Begin(ctx, shardID)
	if err != nil {
		return domain.Reading{}, err
	}
	rd.ShardID = shardID
	rd.LineNo = lease.LineNo()
	payload, err := json.Marshal(rd)
	if err != nil {
		lease.Rollback(ctx)
		return domain.Reading{}, err
	}
	if err := lease.AppendLine(ctx, payload); err != nil {
		lease.Rollback(ctx)
		return domain.Reading{}, err
	}
	if err := r.commitIndex(ctx, rd, payload); err != nil {
		lease.Rollback(ctx)
		return domain.Reading{}, err
	}
	lease.Commit()
	return rd, nil
}

// commitIndex updates the query index and the shard manifest inside a single
// transaction. It is called only after the shard line has been durably written;
// on failure the caller truncates the shard back so the file and index never
// disagree.
func (r *readingRepo) commitIndex(ctx context.Context, rd domain.Reading, payload []byte) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var prevChecksum string
	var firstTS string
	err = tx.QueryRowContext(ctx, `SELECT checksum, COALESCE(first_ts,'') FROM reading_shards WHERE shard_id = ?`, rd.ShardID).Scan(&prevChecksum, &firstTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return err
	}
	nextChecksum := shard.FoldChecksum(prevChecksum, payload)
	if _, err := tx.ExecContext(ctx, `INSERT INTO reading_index (id,collector_id,ts,queue_count,duration_ms,fault_code,shard_id,line_no,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		string(rd.ID), string(rd.CollectorID), timestamp(rd.Timestamp), rd.QueueCount, rd.DurationMs,
		rd.FaultCode, rd.ShardID, rd.LineNo, timestamp(rd.IngestedAt)); err != nil {
		tx.Rollback()
		return err
	}
	first := firstTS
	if first == "" {
		first = timestamp(rd.Timestamp)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reading_shards (shard_id,path,bucket,first_ts,last_ts,count,checksum,status,created_at)
		VALUES (?, ?, 'readings', ?, ?, 1, ?, 'ok', ?)
		ON CONFLICT(shard_id) DO UPDATE SET
			count = reading_shards.count + 1,
			last_ts = excluded.last_ts,
			checksum = excluded.checksum,
			status = 'ok'`,
		rd.ShardID, r.shardPath(rd.ShardID), first, timestamp(rd.Timestamp), nextChecksum, timestamp(r.Now())); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *readingRepo) shardPath(shardID string) string {
	p, err := r.shards.AbsPath(shardID)
	if err != nil {
		return shardID
	}
	return p
}

func (r *readingRepo) Get(ctx context.Context, id domain.ReadingID) (domain.Reading, error) {
	var shardID string
	var lineNo int64
	err := r.db.QueryRowContext(ctx, `SELECT shard_id, line_no FROM reading_index WHERE id = ?`, string(id)).Scan(&shardID, &lineNo)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Reading{}, &NotFoundError{Entity: "reading", ID: string(id)}
	}
	if err != nil {
		return domain.Reading{}, err
	}
	var rd domain.Reading
	if err := r.shards.ReadLine(ctx, shardID, lineNo, &rd); err != nil {
		return domain.Reading{}, fmt.Errorf("read shard %s line %d: %w", shardID, lineNo, err)
	}
	return rd, nil
}

func (r *readingRepo) List(ctx context.Context, f ReadingFilter) ([]domain.ReadingSummary, int64, error) {
	where, args := readingWhere(f)
	return paginatedQuery(ctx, r.db, "reading_index", "id,collector_id,ts,queue_count,duration_ms,fault_code,shard_id,line_no", "ts DESC", where, args, f.Page, scanSummary)
}

func (r *readingRepo) Recent(ctx context.Context, collectorID domain.CollectorID, limit int) ([]domain.ReadingSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,collector_id,ts,queue_count,duration_ms,fault_code,shard_id,line_no
		FROM reading_index WHERE collector_id = ? ORDER BY ts DESC LIMIT ?`, string(collectorID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ReadingSummary, 0)
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *readingRepo) Export(ctx context.Context, f ReadingFilter, fn func(domain.Reading) error) error {
	where, args := readingWhere(f)
	rows, err := r.db.QueryContext(ctx, `SELECT shard_id, line_no FROM reading_index WHERE `+where+` ORDER BY ts ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var shardID string
		var lineNo int64
		if err := rows.Scan(&shardID, &lineNo); err != nil {
			return err
		}
		var rd domain.Reading
		if err := r.shards.ReadLine(ctx, shardID, lineNo, &rd); err != nil {
			r.log.Warn("export skipping unreadable reading", "shard", shardID, "line", lineNo, "err", err)
			continue
		}
		if err := fn(rd); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *readingRepo) Manifest(ctx context.Context) ([]ShardManifest, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT shard_id,path,bucket,COALESCE(first_ts,''),COALESCE(last_ts,''),count,checksum,status,created_at FROM reading_shards ORDER BY shard_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ShardManifest, 0)
	for rows.Next() {
		var m ShardManifest
		var first, last, created string
		if err := rows.Scan(&m.ShardID, &m.Path, &m.Bucket, &first, &last, &m.Count, &m.Checksum, &m.Status, &created); err != nil {
			return nil, err
		}
		m.FirstTS = parseTS(sql.NullString{String: first, Valid: first != ""})
		m.LastTS = parseTS(sql.NullString{String: last, Valid: last != ""})
		m.CreatedAt = parseTS(sql.NullString{String: created, Valid: created != ""})
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *readingRepo) VerifyShard(ctx context.Context, shardID string) (ShardVerifyResult, error) {
	var manifestChecksum string
	var count int64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(checksum,''), count FROM reading_shards WHERE shard_id = ?`, shardID).Scan(&manifestChecksum, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return ShardVerifyResult{ShardID: shardID, Error: "shard not in manifest"}, nil
	}
	if err != nil {
		return ShardVerifyResult{}, err
	}
	stat, err := r.shards.Recompute(ctx, shardID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ShardVerifyResult{ShardID: shardID, Count: 0, ManifestChecksum: manifestChecksum, Error: "shard file missing"}, nil
		}
		return ShardVerifyResult{}, err
	}
	return ShardVerifyResult{
		ShardID:          shardID,
		Count:            stat.Count,
		ManifestChecksum: manifestChecksum,
		ActualChecksum:   stat.Checksum,
		OK:               stat.Checksum == manifestChecksum && stat.Count == count,
	}, nil
}

// RebuildIndex recreates the query index and manifest by scanning every shard on
// disk. Corrupted shards are skipped and reported so a single bad file never
// blocks the rest of the rebuild.
func (r *readingRepo) RebuildIndex(ctx context.Context) (RebuildResult, error) {
	result := RebuildResult{}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reading_index`); err != nil {
		tx.Rollback()
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reading_shards`); err != nil {
		tx.Rollback()
		return result, err
	}
	ids, err := r.shards.ListShards(ctx, "readings")
	if err != nil {
		tx.Rollback()
		return result, err
	}
	for _, sid := range ids {
		if err := ctx.Err(); err != nil {
			tx.Rollback()
			return result, err
		}
		stat, err := r.shards.Recompute(ctx, sid)
		if err != nil {
			r.log.Warn("rebuild skipping unreadable shard", "shard", sid, "err", err)
			result.Skipped = append(result.Skipped, sid)
			continue
		}
		if err := r.rebuildShardRows(ctx, tx, sid, stat); err != nil {
			tx.Rollback()
			return result, err
		}
		result.Shards++
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.Rows, err = r.countIndex(ctx)
	return result, err
}

func (r *readingRepo) rebuildShardRows(ctx context.Context, tx *sql.Tx, sid string, stat shard.Stat) error {
	var firstTS, lastTS string
	_, err := r.shards.Scan(ctx, sid, func(lineNo int64, data []byte) error {
		var rd domain.Reading
		if err := json.Unmarshal(data, &rd); err != nil {
			return nil
		}
		if firstTS == "" {
			firstTS = timestamp(rd.Timestamp)
		}
		lastTS = timestamp(rd.Timestamp)
		if _, err := tx.ExecContext(ctx, `INSERT INTO reading_index (id,collector_id,ts,queue_count,duration_ms,fault_code,shard_id,line_no,created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			string(rd.ID), string(rd.CollectorID), timestamp(rd.Timestamp), rd.QueueCount, rd.DurationMs,
			rd.FaultCode, sid, lineNo, timestamp(rd.IngestedAt)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reading_shards (shard_id,path,bucket,first_ts,last_ts,count,checksum,status,created_at)
		VALUES (?, ?, 'readings', ?, ?, ?, ?, 'ok', ?)`,
		sid, r.shardPath(sid), firstTS, lastTS, stat.Count, stat.Checksum, timestamp(r.Now())); err != nil {
		return err
	}
	return nil
}

func (r *readingRepo) countIndex(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reading_index`).Scan(&n)
	return n, err
}

func readingWhere(f ReadingFilter) (string, []any) {
	var where []string
	var args []any
	eq(&where, &args, "collector_id", string(f.CollectorID))
	if !f.From.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, timestamp(f.From))
	}
	if !f.To.IsZero() {
		where = append(where, "ts <= ?")
		args = append(args, timestamp(f.To))
	}
	eq(&where, &args, "fault_code", f.FaultCode)
	return buildWhere(where), args
}

func scanSummary(sc rowScanner) (domain.ReadingSummary, error) {
	var s domain.ReadingSummary
	var id, collector, ts, fault, shardID string
	err := sc.Scan(&id, &collector, &ts, &s.QueueCount, &s.DurationMs, &fault, &shardID, &s.LineNo)
	if err != nil {
		return domain.ReadingSummary{}, err
	}
	s.ID = domain.ReadingID(id)
	s.CollectorID = domain.CollectorID(collector)
	s.Timestamp = parseTS(sql.NullString{String: ts, Valid: ts != ""})
	s.FaultCode = fault
	s.ShardID = shardID
	return s, nil
}

// Total returns the number of indexed readings.
func (r *readingRepo) Total(ctx context.Context) (int64, error) {
	return r.countIndex(ctx)
}
