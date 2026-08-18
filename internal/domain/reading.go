package domain

import "time"

// ReadingID uniquely identifies a single operational reading.
type ReadingID string

// Reading is a single operational snapshot reported by a collector: current
// queue length, latest case processing time and an optional fault code. The
// authoritative copy lives in a sharded JSONL file; only the projection below
// is mirrored into the query index so list and filter stay cheap.
type Reading struct {
	ID          ReadingID          `json:"id"`
	CollectorID CollectorID        `json:"collector_id"`
	Timestamp   time.Time          `json:"timestamp"`
	QueueCount  int                `json:"queue_count"`
	DurationMs  int64              `json:"duration_ms"`
	FaultCode   string             `json:"fault_code"`
	RawMetrics  map[string]float64 `json:"raw_metrics,omitempty"`
	Source      string             `json:"source,omitempty"`
	Seq         int64              `json:"seq,omitempty"`
	ShardID     string             `json:"shard_id"`
	LineNo      int64              `json:"line_no"`
	IngestedAt  time.Time          `json:"ingested_at"`
}

// ReadingSummary is the lightweight projection stored in the query index. It
// carries enough to render lists and to drive alert evaluation; the full
// reading (with raw metrics) is fetched from its shard on demand.
type ReadingSummary struct {
	ID          ReadingID   `json:"id"`
	CollectorID CollectorID `json:"collector_id"`
	Timestamp   time.Time   `json:"timestamp"`
	QueueCount  int         `json:"queue_count"`
	DurationMs  int64       `json:"duration_ms"`
	FaultCode   string      `json:"fault_code"`
	ShardID     string      `json:"shard_id"`
	LineNo      int64       `json:"line_no"`
}
