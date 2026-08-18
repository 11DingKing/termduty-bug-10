package domain

import "time"

// IngestID uniquely identifies a queued inbound reading.
type IngestID string

// IngestStatus tracks an inbound reading through the poll-pull pipeline.
type IngestStatus string

const (
	IngestPending IngestStatus = "pending"
	IngestLeased  IngestStatus = "leased"
	IngestDone    IngestStatus = "done"
	IngestFailed  IngestStatus = "failed"
)

func (s IngestStatus) Valid() bool {
	switch s {
	case IngestPending, IngestLeased, IngestDone, IngestFailed:
		return true
	}
	return false
}

// IngestItem is an inbound reading waiting to be pulled by the duty collector
// worker. Leasing lets a worker claim a batch; if the worker stalls the lease
// expires and a later tick reclaims the item so work is never lost.
type IngestItem struct {
	ID             IngestID
	CollectorID    CollectorID
	Payload        []byte
	Status         IngestStatus
	LeasedAt       time.Time
	LeaseExpiresAt time.Time
	Attempts       int
	LastError      string
	ShardID        string
	LineNo         int64
	CreatedAt      time.Time
}

// IsReclaimable reports whether a leased item's lease has expired.
func (i IngestItem) IsReclaimable(now time.Time) bool {
	return i.Status == IngestLeased && !i.LeaseExpiresAt.IsZero() && !now.Before(i.LeaseExpiresAt)
}

// ReadingSubmission is the inbound payload a collector pushes to the ingest
// endpoint before it is queued for the duty collector worker to pull.
type ReadingSubmission struct {
	CollectorID CollectorID
	Timestamp   time.Time
	QueueCount  int
	DurationMs  int64
	FaultCode   string
	RawMetrics  map[string]float64
	Source      string
	Seq         int64
}

// Validate checks the submission is well-formed before it is accepted.
func (s ReadingSubmission) Validate() error {
	if s.CollectorID == "" {
		return ErrValidation
	}
	if s.QueueCount < 0 {
		return ErrValidation
	}
	if s.DurationMs < 0 {
		return ErrValidation
	}
	return nil
}

// ToReading materialises a submission into a reading record.
func (s ReadingSubmission) ToReading(now time.Time) Reading {
	ts := s.Timestamp
	if ts.IsZero() {
		ts = now
	}
	return Reading{
		CollectorID: s.CollectorID,
		Timestamp:   ts,
		QueueCount:  s.QueueCount,
		DurationMs:  s.DurationMs,
		FaultCode:   s.FaultCode,
		RawMetrics:  s.RawMetrics,
		Source:      s.Source,
		Seq:         s.Seq,
		IngestedAt:  now,
	}
}
