package orchestration_test

import (
	"context"
	"testing"
	"time"

	"termduty/internal/domain"
	"termduty/internal/orchestration"
)

// TestBatchDisable_RollbackOnFailure verifies a failed batch rolls back the
// already-disabled collectors so none stay half-disabled.
func TestBatchDisable_RollbackOnFailure(t *testing.T) {
	svc, _, st := newSvc(t)
	c1 := seedCollector(t, st, "B1")
	c2 := seedCollector(t, st, "B2")
	c3 := seedCollector(t, st, "B3")
	ops := orchestration.Actor{ID: "ops", Role: domain.RoleOps}
	_, err := svc.Batch.DisableCollectors(context.Background(), []domain.CollectorID{c1.ID, "col-ghost", c3.ID}, ops)
	if err == nil {
		t.Fatalf("expected failure on missing collector")
	}
	for _, c := range []domain.CollectorID{c1.ID, c2.ID, c3.ID} {
		got, _ := st.Collectors().Get(context.Background(), c)
		if !got.IsCollecting() {
			t.Fatalf("collector %s left disabled after rollback", c)
		}
	}
}

// TestBatchDisable_IdempotentRetrySucceeds confirms the whole batch can be
// retried cleanly after a rollback, with no double-effect.
func TestBatchDisable_IdempotentRetrySucceeds(t *testing.T) {
	svc, _, st := newSvc(t)
	c1 := seedCollector(t, st, "R1")
	c2 := seedCollector(t, st, "R2")
	c3 := seedCollector(t, st, "R3")
	ops := orchestration.Actor{ID: "ops", Role: domain.RoleOps}
	if _, err := svc.Batch.DisableCollectors(context.Background(), []domain.CollectorID{c1.ID, "ghost"}, ops); err == nil {
		t.Fatalf("expected failure")
	}
	completed, err := svc.Batch.DisableCollectors(context.Background(), []domain.CollectorID{c1.ID, c2.ID, c3.ID}, ops)
	if err != nil || completed != 3 {
		t.Fatalf("retry: completed=%d err=%v", completed, err)
	}
	for _, c := range []domain.CollectorID{c1.ID, c2.ID, c3.ID} {
		got, _ := st.Collectors().Get(context.Background(), c)
		if got.IsCollecting() {
			t.Fatalf("collector %s not disabled after retry", c)
		}
	}
	// Re-running the same batch is idempotent: already-disabled collectors stay disabled.
	if _, err := svc.Batch.DisableCollectors(context.Background(), []domain.CollectorID{c1.ID, c2.ID, c3.ID}, ops); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// TestBatchRules_RollbackCompensation verifies a failed rule tune restores the
// prior rule values.
func TestBatchRules_RollbackCompensation(t *testing.T) {
	svc, _, st := newSvc(t)
	max8 := 8.0
	r1 := domain.Rule{ID: "rule-cb1", Metric: domain.MetricQueueCount, MaxValue: &max8, WindowSeconds: 60,
		Severity: domain.SeverityWarn, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	r2 := domain.Rule{ID: "rule-cb2", Metric: domain.MetricQueueCount, MaxValue: &max8, WindowSeconds: 60,
		Severity: domain.SeverityWarn, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = st.Rules().Create(context.Background(), r1)
	_ = st.Rules().Create(context.Background(), r2)
	ops := orchestration.Actor{ID: "ops", Role: domain.RoleOps}
	max50 := 50.0
	disabled := false
	updates := []orchestration.RuleUpdate{
		{ID: r1.ID, MaxValue: &max50},
		{ID: "rule-missing", Enabled: &disabled},
		{ID: r2.ID, MaxValue: &max50},
	}
	if _, err := svc.Batch.UpdateRules(context.Background(), updates, ops); err == nil {
		t.Fatalf("expected failure on missing rule")
	}
	got1, _ := st.Rules().Get(context.Background(), r1.ID)
	if got1.MaxValue == nil || *got1.MaxValue != 8 {
		t.Fatalf("rule r1 not restored: %+v", got1.MaxValue)
	}
	got2, _ := st.Rules().Get(context.Background(), r2.ID)
	if got2.MaxValue == nil || *got2.MaxValue != 8 {
		t.Fatalf("rule r2 not restored: %+v", got2.MaxValue)
	}
}
