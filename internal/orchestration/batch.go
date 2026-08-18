package orchestration

import (
	"context"
	"fmt"
	"log/slog"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// BatchService performs multi-object operations with saga-style compensation:
// every step records an idempotent compensating action, and on the first
// failure the completed steps are rolled back in reverse so the batch is
// all-or-nothing and safe to retry.
type BatchService struct {
	store *store.Store
	clock domain.Clock
	audit *AuditRecorder
	log   *slog.Logger
}

// Step is one unit of a batch operation together with its compensation.
type Step struct {
	Name       string
	Do         func(context.Context) error
	Compensate func(context.Context) error
}

// RunBatch executes steps in order. On the first failure it runs the
// compensations of the already-completed steps in reverse order. Compensations
// are idempotent so a partially-applied compensate pass can be retried safely.
func RunBatch(ctx context.Context, steps []Step) (int, error) {
	for i, st := range steps {
		if err := st.Do(ctx); err != nil {
			if cerr := compensate(ctx, steps[:i]); cerr != nil {
				return i, fmt.Errorf("step %d %s: %w; compensation failed: %v", i, st.Name, err, cerr)
			}
			return i, fmt.Errorf("step %d %s: %w", i, st.Name, err)
		}
	}
	return len(steps), nil
}

func compensate(ctx context.Context, steps []Step) error {
	var first error
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Compensate == nil {
			continue
		}
		if err := steps[i].Compensate(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// DisableCollectors disables a set of collection points. If any collector is
// missing the whole batch is rolled back so operators can fix the input and
// retry without leaving collectors half-disabled.
func (s *BatchService) DisableCollectors(ctx context.Context, ids []domain.CollectorID, actor Actor) (int, error) {
	steps := make([]Step, 0, len(ids))
	for _, id := range ids {
		steps = append(steps, s.disableStep(id))
	}
	completed, err := RunBatch(ctx, steps)
	if err != nil {
		s.audit.bestEffort(ctx, actor.ID, actor.Role, "batch.disable.failed", "collector", "", map[string]any{
			"requested": len(ids), "completed": completed, "error": err.Error(),
		})
		return completed, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "batch.disable", "collector", "", map[string]any{"count": completed})
	return completed, nil
}

func (s *BatchService) disableStep(id domain.CollectorID) Step {
	var prev domain.CollectorStatus
	return Step{
		Name: "disable:" + string(id),
		Do: func(ctx context.Context) error {
			c, err := s.store.Collectors().Get(ctx, id)
			if err != nil {
				return err
			}
			if !c.IsCollecting() {
				return nil
			}
			prev = c.Status
			_, err = s.store.Collectors().SetStatus(ctx, id, domain.CollectorStatusDisabled, c.Version)
			return err
		},
		Compensate: func(ctx context.Context) error {
			c, err := s.store.Collectors().Get(ctx, id)
			if err != nil {
				return nil
			}
			if c.IsCollecting() {
				return nil
			}
			target := prev
			if target == "" {
				target = domain.CollectorStatusActive
			}
			_, err = s.store.Collectors().SetStatus(ctx, id, target, c.Version)
			return err
		},
	}
}

// RuleUpdate is one rule adjustment inside a batch tune.
type RuleUpdate struct {
	ID           string
	MinValue     *float64
	MaxValue     *float64
	Enabled      *bool
	Severity     domain.Severity
	FaultTrigger *string
}

// UpdateRules applies a batch of agreed-range adjustments. Each step captures
// the prior values and reverts them on rollback, so a failed tune restores
// every rule to its pre-batch state.
func (s *BatchService) UpdateRules(ctx context.Context, updates []RuleUpdate, actor Actor) (int, error) {
	steps := make([]Step, 0, len(updates))
	for _, u := range updates {
		steps = append(steps, s.ruleStep(u))
	}
	completed, err := RunBatch(ctx, steps)
	if err != nil {
		s.audit.bestEffort(ctx, actor.ID, actor.Role, "batch.rules.failed", "rule", "", map[string]any{
			"requested": len(updates), "completed": completed, "error": err.Error(),
		})
		return completed, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "batch.rules", "rule", "", map[string]any{"count": completed})
	return completed, nil
}

func (s *BatchService) ruleStep(u RuleUpdate) Step {
	var prev domain.Rule
	havePrev := false
	return Step{
		Name: "rule:" + u.ID,
		Do: func(ctx context.Context) error {
			r, err := s.store.Rules().Get(ctx, u.ID)
			if err != nil {
				return err
			}
			prev = r
			havePrev = true
			applyRuleUpdate(&r, u)
			return s.store.Rules().Update(ctx, &r)
		},
		Compensate: func(ctx context.Context) error {
			if !havePrev {
				return nil
			}
			cur, err := s.store.Rules().Get(ctx, u.ID)
			if err != nil {
				return nil
			}
			prev.Version = cur.Version
			return s.store.Rules().Update(ctx, &prev)
		},
	}
}

func applyRuleUpdate(r *domain.Rule, u RuleUpdate) {
	if u.MinValue != nil {
		r.MinValue = u.MinValue
	}
	if u.MaxValue != nil {
		r.MaxValue = u.MaxValue
	}
	if u.Enabled != nil {
		r.Enabled = *u.Enabled
	}
	if u.Severity != "" {
		r.Severity = u.Severity
	}
	if u.FaultTrigger != nil {
		r.FaultTrigger = *u.FaultTrigger
	}
}

// ReinjectFailure re-queues a dead-lettered item for processing.
func (s *BatchService) ReinjectFailure(ctx context.Context, id string, actor Actor) (store.PermanentFailure, error) {
	f, err := s.store.Failures().Requeue(ctx, id)
	if err != nil {
		return store.PermanentFailure{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "failure.reinject", "permanent_failure", id, map[string]any{
		"task": f.TaskType, "target": f.TargetID,
	})
	return f, nil
}

// ResolveFailure marks a dead-lettered item resolved without re-processing.
func (s *BatchService) ResolveFailure(ctx context.Context, id string, actor Actor) error {
	if err := s.store.Failures().Resolve(ctx, id); err != nil {
		return err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "failure.resolve", "permanent_failure", id, nil)
	return nil
}

// ListFailures returns a page of permanent failures.
func (s *BatchService) ListFailures(ctx context.Context, f store.FailureFilter) ([]store.PermanentFailure, int64, error) {
	return s.store.Failures().List(ctx, f)
}
