package domain_test

import (
	"errors"
	"testing"

	"termduty/internal/domain"
)

// TestNextState_LegalTransitions walks the happy path through the table.
func TestNextState_LegalTransitions(t *testing.T) {
	cases := []struct {
		from  domain.AlertState
		event domain.AlertEvent
		want  domain.AlertState
	}{
		{domain.AlertStateOpen, domain.EventAccept, domain.AlertStateAssigned},
		{domain.AlertStateAssigned, domain.EventStart, domain.AlertStateHandling},
		{domain.AlertStateHandling, domain.EventResolve, domain.AlertStateResolved},
		{domain.AlertStateResolved, domain.EventClose, domain.AlertStateClosed},
		{domain.AlertStateOpen, domain.EventRevoke, domain.AlertStateRevoked},
		{domain.AlertStateAssigned, domain.EventRevoke, domain.AlertStateRevoked},
		{domain.AlertStateHandling, domain.EventRevoke, domain.AlertStateRevoked},
		{domain.AlertStateAssigned, domain.EventRelease, domain.AlertStateOpen},
		{domain.AlertStateHandling, domain.EventRelease, domain.AlertStateOpen},
	}
	for _, c := range cases {
		got, err := domain.NextState(c.from, c.event)
		if err != nil || got != c.want {
			t.Errorf("%s --%s--> got=%s err=%v want=%s", c.from, c.event, got, err, c.want)
		}
	}
}

// TestNextState_IllegalTransitionRejected ensures moves absent from the table
// fail with ErrIllegalTransition.
func TestNextState_IllegalTransitionRejected(t *testing.T) {
	cases := []struct {
		from  domain.AlertState
		event domain.AlertEvent
	}{
		{domain.AlertStateOpen, domain.EventResolve},
		{domain.AlertStateOpen, domain.EventClose},
		{domain.AlertStateOpen, domain.EventStart},
		{domain.AlertStateClosed, domain.EventAccept},
		{domain.AlertStateClosed, domain.EventRevoke},
		{domain.AlertStateRevoked, domain.EventAccept},
		{domain.AlertStateResolved, domain.EventAccept},
		{domain.AlertStateResolved, domain.EventStart},
	}
	for _, c := range cases {
		_, err := domain.NextState(c.from, c.event)
		if !errors.Is(err, domain.ErrIllegalTransition) {
			t.Errorf("%s --%s--> want ErrIllegalTransition, got %v", c.from, c.event, err)
		}
	}
}

// TestLegalTransitions_NonTerminalHasActions confirms open alerts expose the
// accept action, used by the UI to render available moves.
func TestLegalTransitions_NonTerminalHasActions(t *testing.T) {
	for _, s := range []domain.AlertState{domain.AlertStateClosed, domain.AlertStateRevoked} {
		if actions := domain.LegalTransitions(s); len(actions) != 0 {
			t.Errorf("terminal %s has actions %v", s, actions)
		}
	}
	if actions := domain.LegalTransitions(domain.AlertStateOpen); len(actions) == 0 {
		t.Errorf("open state has no actions")
	}
}

// TestRuleEvaluate_BreachDetection exercises the agreed-range rule logic.
func TestRuleEvaluate_BreachDetection(t *testing.T) {
	max := 8.0
	r := domain.Rule{Enabled: true, Metric: domain.MetricQueueCount, MaxValue: &max}
	if breached, _ := r.Evaluate(domain.ReadingSummary{QueueCount: 5}); breached {
		t.Fatalf("5 should not breach max 8")
	}
	if breached, _ := r.Evaluate(domain.ReadingSummary{QueueCount: 20}); !breached {
		t.Fatalf("20 should breach max 8")
	}
	r.Enabled = false
	if breached, _ := r.Evaluate(domain.ReadingSummary{QueueCount: 20}); breached {
		t.Fatalf("disabled rule should not breach")
	}
}

// TestCollectorTransition_IllegalRejects covers collector enable/disable moves.
func TestCollectorTransition_IllegalRejects(t *testing.T) {
	c := domain.Collector{Status: domain.CollectorStatusActive}
	if _, err := c.Transition(domain.CollectorStatusActive); !errors.Is(err, domain.ErrIllegalTransition) {
		t.Fatalf("active->active: want ErrIllegalTransition, got %v", err)
	}
	if prev, err := c.Transition(domain.CollectorStatusDisabled); err != nil || prev != domain.CollectorStatusActive {
		t.Fatalf("active->disabled: %v prev=%s", err, prev)
	}
	if _, err := c.Transition("bogus"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("bogus: want ErrValidation, got %v", err)
	}
}

// TestIngestItem_IsReclaimable covers lease expiry logic.
func TestIngestItem_IsReclaimable(t *testing.T) {
	now := parseFake("2026-08-18T12:05:00Z")
	item := domain.IngestItem{Status: domain.IngestLeased, LeaseExpiresAt: parseFake("2026-08-18T12:00:00Z")}
	if !item.IsReclaimable(now) {
		t.Fatalf("expired lease should be reclaimable")
	}
	item2 := domain.IngestItem{Status: domain.IngestPending, LeaseExpiresAt: parseFake("2026-08-18T12:00:00Z")}
	if item2.IsReclaimable(now) {
		t.Fatalf("pending item is not reclaimable")
	}
}
