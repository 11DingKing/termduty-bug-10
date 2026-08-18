package orchestration

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// IngestService accepts inbound readings into the poll-pull queue and processes
// leased items into the sharded reading store. Processing is idempotent: a
// completed item is never re-applied, and a crashed worker's lease expires so
// the next tick reclaims the item.
type IngestService struct {
	store  *store.Store
	clock  domain.Clock
	audit  *AuditRecorder
	log    *slog.Logger
	policy RetryPolicy
}

// Enqueue validates a submission and places it on the ingest queue.
func (s *IngestService) Enqueue(ctx context.Context, sub domain.ReadingSubmission, actor Actor) (domain.IngestID, error) {
	if err := sub.Validate(); err != nil {
		return "", err
	}
	c, err := s.store.Collectors().Get(ctx, sub.CollectorID)
	if err != nil {
		return "", err
	}
	if !c.IsCollecting() {
		return "", domain.ErrCollectorDisabled
	}
	payload, err := json.Marshal(sub)
	if err != nil {
		return "", err
	}
	item := domain.IngestItem{
		ID:          domain.IngestID(newID()),
		CollectorID: sub.CollectorID,
		Payload:     payload,
		Status:      domain.IngestPending,
		CreatedAt:   s.clock.Now(),
	}
	if err := s.store.Ingest().Enqueue(ctx, item); err != nil {
		return "", err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "ingest.enqueue", "collector", string(sub.CollectorID), map[string]any{
		"queue_count": sub.QueueCount, "duration_ms": sub.DurationMs, "fault_code": sub.FaultCode,
	})
	return item.ID, nil
}

// ProcessBatch is the poll-pull step: it leases a batch, persists each reading
// to a shard and the index, and closes the loop on the queue. Items that exceed
// the retry policy are dead-lettered so an operator can re-inject them later.
func (s *IngestService) ProcessBatch(ctx context.Context, now time.Time, ttl time.Duration, limit int) (BatchSummary, error) {
	summary := BatchSummary{}
	items, err := s.store.Ingest().LeaseBatch(ctx, now, ttl, limit)
	if err != nil {
		return summary, err
	}
	summary.Leased = len(items)
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary, _ = s.processOne(ctx, it, summary)
	}
	return summary, nil
}

func (s *IngestService) processOne(ctx context.Context, it domain.IngestItem, summary BatchSummary) (BatchSummary, error) {
	var sub domain.ReadingSubmission
	if err := json.Unmarshal(it.Payload, &sub); err != nil {
		summary.Failed++
		summary.Errors = append(summary.Errors, err.Error())
		_ = s.store.Ingest().Fail(ctx, it.ID, err.Error())
		s.deadLetter(ctx, it, err)
		summary.DeadLettered++
		return summary, nil
	}
	rd := sub.ToReading(nowFromClock(s.clock, it))
	reading, err := s.store.Readings().Append(ctx, rd)
	if err != nil {
		summary.Failed++
		summary.Errors = append(summary.Errors, err.Error())
		_ = s.store.Ingest().Fail(ctx, it.ID, err.Error())
		if it.Attempts >= s.policy.MaxAttempts {
			s.deadLetter(ctx, it, err)
			summary.DeadLettered++
		}
		return summary, nil
	}
	if err := s.store.Ingest().Complete(ctx, it.ID, reading.ShardID, reading.LineNo); err != nil {
		summary.Errors = append(summary.Errors, err.Error())
		return summary, nil
	}
	summary.Processed++
	return summary, nil
}

// deadLetter records a permanent failure for an ingest item that exhausted its
// retries, carrying the last error, attempt count and time so it can be queried
// and re-injected.
func (s *IngestService) deadLetter(ctx context.Context, it domain.IngestItem, cause error) {
	fail := store.PermanentFailure{
		TaskType:  "ingest",
		TargetID:  string(it.ID),
		Payload:   string(it.Payload),
		LastError: cause.Error(),
		Attempts:  it.Attempts,
		Status:    "dead",
		FailedAt:  s.clock.Now(),
	}
	if err := s.store.Failures().Record(ctx, fail); err != nil && s.log != nil {
		s.log.Warn("dead-letter record failed", "target", it.ID, "err", err)
	}
	s.audit.bestEffort(ctx, "system", domain.RoleSystem, "ingest.dead_letter", "ingest", string(it.ID), map[string]any{
		"error": cause.Error(), "attempts": it.Attempts,
	})
}

// Reclaim returns expired leases to pending so stranded work is picked up again.
func (s *IngestService) Reclaim(ctx context.Context, now time.Time) (int64, error) {
	return s.store.Ingest().ReclaimExpired(ctx, now)
}

func nowFromClock(clock domain.Clock, it domain.IngestItem) time.Time {
	if !it.LeasedAt.IsZero() {
		return it.LeasedAt
	}
	return clock.Now()
}
