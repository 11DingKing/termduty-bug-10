// Package orchestration wires the domain rules to the persistence layer. It is
// the only place that knows how alerts are raised, accepted, resolved and
// revoked, how readings are pulled and indexed, and how multi-object batch
// operations commit or compensate. The HTTP and scheduler layers depend on these
// services and never touch storage directly.
package orchestration

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// Actor identifies who is performing an operation. The duty desk may only
// observe and judge, responsible persons may only handle, and center operations
// may bulk-tune agreed ranges or disable collection points.
type Actor struct {
	ID   string
	Role domain.Role
}

// Services is the application facade exposed to the HTTP and scheduler layers.
type Services struct {
	Store  *store.Store
	Ingest *IngestService
	Alerts *AlertService
	Batch  *BatchService
	Admin  *AdminService
	Query  *QueryService
	clock  domain.Clock
	log    *slog.Logger
	audit  *AuditRecorder
	policy RetryPolicy
}

// New builds the service graph on top of a store, sharing one clock and logger.
func New(s *store.Store, clock domain.Clock, log *slog.Logger) *Services {
	audit := &AuditRecorder{repo: s.Audit(), clock: clock, log: log}
	policy := DefaultRetryPolicy()
	return &Services{
		Store: s, clock: clock, log: log, audit: audit, policy: policy,
		Ingest: &IngestService{store: s, clock: clock, audit: audit, log: log, policy: policy},
		Alerts: &AlertService{store: s, clock: clock, audit: audit, log: log},
		Batch:  &BatchService{store: s, clock: clock, audit: audit, log: log},
		Admin:  &AdminService{store: s, clock: clock, audit: audit, log: log},
		Query:  &QueryService{store: s, clock: clock, log: log},
	}
}

// Clock exposes the shared clock for the scheduler.
func (s *Services) Clock() domain.Clock { return s.clock }

// Policy exposes the shared retry policy for the scheduler.
func (s *Services) Policy() RetryPolicy { return s.policy }

// RetryPolicy governs background task retries: a bounded number of attempts with
// exponential backoff and optional jitter, after which work is dead-lettered.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      func() float64
}

// DefaultRetryPolicy returns a production-shaped policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: defaultJitter}
}

func defaultJitter() float64 { return 0.5 }

// NextBackoff returns the delay before the next attempt. It doubles per attempt
// and applies the configured jitter so concurrent retriers do not thunder.
func (p RetryPolicy) NextBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if p.BaseDelay <= 0 {
		return 0
	}
	d := p.BaseDelay
	for i := 1; i < attempt && d < p.MaxDelay; i++ {
		d *= 2
	}
	if d > p.MaxDelay {
		d = p.MaxDelay
	}
	if p.Jitter != nil {
		j := p.Jitter()
		d = time.Duration(float64(d) * (0.9 + 0.2*j))
	}
	return d
}

// BatchSummary reports what a poll-pull cycle achieved.
type BatchSummary struct {
	Leased       int
	Processed    int
	Failed       int
	DeadLettered int
	Errors       []string
}

// EvalSummary reports what an alert evaluation sweep achieved.
type EvalSummary struct {
	Collectors int
	Evaluated  int
	Created    int
	Suppressed int
}

// BacklogSummary reports outstanding and overdue work for the duty desk.
type BacklogSummary struct {
	OpenAlerts     int64
	AssignedAlerts int64
	HandlingAlerts int64
	OverdueAlerts  []domain.Alert
	PendingIngest  int64
	LeasedIngest   int64
	DeadLettered   int
	Failures       []store.PermanentFailure
}

// StatsSummary aggregates headline counters for the overview.
type StatsSummary struct {
	Collectors    int64
	ActiveAlerts  int64
	AlertsByState map[domain.AlertState]int64
	Readings      int64
	PendingIngest int64
	LeasedIngest  int64
	DeadLettered  int64
	Shards        int
}

// newID generates a fresh identifier for entities created by orchestration.
func newID() string { return uuid.NewString() }

func boolPtr(b bool) *bool { return &b }

// AuditRecorder is a thin helper that stamps audit entries with the shared
// clock. Recording never aborts a business operation: a failure is logged so the
// operator can rebuild the trail from structured logs if necessary.
type AuditRecorder struct {
	repo  store.AuditRepo
	clock domain.Clock
	log   *slog.Logger
}

// Record appends an audit entry. It returns the error so callers in tests can
// assert, but production callers ignore it via bestEffort.
func (a *AuditRecorder) Record(ctx context.Context, actor string, role domain.Role, action, targetType, targetID string, detail map[string]any) error {
	if a == nil || a.repo == nil {
		return nil
	}
	return a.repo.Append(ctx, domain.AuditEntry{
		Actor:      actor,
		Role:       role,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
		At:         a.clock.Now(),
	})
}

// bestEffort records an audit entry and logs (never returns) any failure.
func (a *AuditRecorder) bestEffort(ctx context.Context, actor string, role domain.Role, action, targetType, targetID string, detail map[string]any) {
	if err := a.Record(ctx, actor, role, action, targetType, targetID, detail); err != nil && a.log != nil {
		a.log.Warn("audit record failed", "action", action, "target", targetID, "err", err)
	}
}
