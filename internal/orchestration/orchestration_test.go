package orchestration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"termduty/internal/domain"
	"termduty/internal/orchestration"
	"termduty/internal/store"
)

func newSvc(t *testing.T) (*orchestration.Services, *domain.FakeClock, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	clock := domain.NewFakeClock(time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.New(context.Background(), filepath.Join(dir, "termduty.db"), filepath.Join(dir, "shards"), clock, log)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return orchestration.New(st, clock, log), clock, st
}

func seedCollector(t *testing.T, st *store.Store, code string) domain.Collector {
	t.Helper()
	c := domain.Collector{
		ID: domain.CollectorID("col-" + code), Code: code, Name: code,
		Kind: domain.CollectorKindWindow, Status: domain.CollectorStatusActive,
		HandlerID: "h-" + code, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.Collectors().Create(context.Background(), c); err != nil {
		t.Fatalf("create collector: %v", err)
	}
	return c
}

func seedRule(t *testing.T, st *store.Store, id string, maxQ float64) domain.Rule {
	t.Helper()
	r := domain.Rule{ID: id, Metric: domain.MetricQueueCount, MaxValue: &maxQ, WindowSeconds: 60,
		Severity: domain.SeverityWarn, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Rules().Create(context.Background(), r); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	return r
}

func appendReading(t *testing.T, st *store.Store, cid domain.CollectorID, ts time.Time, queue int) domain.Reading {
	t.Helper()
	rd := domain.Reading{ID: domain.ReadingID("rd-" + string(cid) + "-" + ts.Format("150405")), CollectorID: cid,
		Timestamp: ts, QueueCount: queue, DurationMs: 100, Source: "test", IngestedAt: ts}
	got, err := st.Readings().Append(context.Background(), rd)
	if err != nil {
		t.Fatalf("append reading: %v", err)
	}
	return got
}

// TestAlertEvaluate_SuppressionIsIdempotent verifies a repeated breach does not
// raise a duplicate alert for the same object and problem.
func TestAlertEvaluate_SuppressionIsIdempotent(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "S1")
	seedRule(t, st, "rule-s1", 8)
	appendReading(t, st, c.ID, clock.Now(), 20)

	s1, err := svc.Alerts.Evaluate(context.Background(), clock.Now())
	if err != nil {
		t.Fatalf("evaluate 1: %v", err)
	}
	if s1.Created != 1 {
		t.Fatalf("created=%d want 1", s1.Created)
	}
	s2, err := svc.Alerts.Evaluate(context.Background(), clock.Now())
	if err != nil {
		t.Fatalf("evaluate 2: %v", err)
	}
	if s2.Created != 0 {
		t.Fatalf("second evaluate created=%d want 0 (suppressed)", s2.Created)
	}
	if s2.Suppressed != 1 {
		t.Fatalf("suppressed=%d want 1", s2.Suppressed)
	}
	alerts, total, _ := svc.Alerts.List(context.Background(), store.AlertFilter{CollectorID: c.ID, Page: domain.Page{Size: 10}})
	if total != 1 {
		t.Fatalf("total alerts=%d want 1", total)
	}
	_ = alerts
}

// TestAlertLifecycle_LegalTransitions walks accept->start->resolve->close.
func TestAlertLifecycle_LegalTransitions(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "L1")
	seedRule(t, st, "rule-l1", 8)
	appendReading(t, st, c.ID, clock.Now(), 20)
	if _, err := svc.Alerts.Evaluate(context.Background(), clock.Now()); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	alerts, _, _ := svc.Alerts.List(context.Background(), store.AlertFilter{State: domain.AlertStateOpen, Page: domain.Page{Size: 10}})
	if len(alerts) != 1 {
		t.Fatalf("open alerts=%d", len(alerts))
	}
	aid := alerts[0].ID
	actor := orchestration.Actor{ID: "h-1", Role: domain.RoleHandler}
	if _, _, err := svc.Alerts.Accept(context.Background(), aid, "h-1", actor); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := svc.Alerts.Start(context.Background(), aid, "h-1", actor); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.Alerts.Resolve(context.Background(), aid, "h-1", "fixed", actor); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	duty := orchestration.Actor{ID: "duty-1", Role: domain.RoleDuty}
	if _, err := svc.Alerts.Close(context.Background(), aid, duty); err != nil {
		t.Fatalf("close: %v", err)
	}
	final, _ := svc.Alerts.Get(context.Background(), aid)
	if final.State != domain.AlertStateClosed {
		t.Fatalf("final state=%s want closed", final.State)
	}
}

// TestAlertResolve_IllegalTransitionRejected confirms resolving an unaccepted
// alert is rejected by the state machine.
func TestAlertResolve_IllegalTransitionRejected(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "I1")
	seedRule(t, st, "rule-i1", 8)
	appendReading(t, st, c.ID, clock.Now(), 20)
	_, _ = svc.Alerts.Evaluate(context.Background(), clock.Now())
	alerts, _, _ := svc.Alerts.List(context.Background(), store.AlertFilter{State: domain.AlertStateOpen, Page: domain.Page{Size: 10}})
	aid := alerts[0].ID
	_, err := svc.Alerts.Resolve(context.Background(), aid, "h-x", "note", orchestration.Actor{ID: "h-x", Role: domain.RoleHandler})
	if !errors.Is(err, domain.ErrIllegalTransition) {
		t.Fatalf("resolve open alert: want ErrIllegalTransition, got %v", err)
	}
}

// TestAlertAccept_LoserLearnsWinner ensures a losing accepter is told exactly
// who took the alert.
func TestAlertAccept_LoserLearnsWinner(t *testing.T) {
	svc, _, st := newSvc(t)
	c := seedCollector(t, st, "W1")
	seedRule(t, st, "rule-w1", 8)
	rd := appendReading(t, st, c.ID, time.Date(2026, 8, 18, 12, 1, 0, 0, time.UTC), 20)
	alert := domain.Alert{ID: domain.AlertID("alert-w1"), CollectorID: c.ID, RuleID: "rule-w1",
		ReadingID: rd.ID, Severity: domain.SeverityWarn, State: domain.AlertStateOpen,
		FirstSeen: time.Now(), LastSeen: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Alerts().Create(context.Background(), alert); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	actor := orchestration.Actor{ID: "h-winner", Role: domain.RoleHandler}
	if _, _, err := svc.Alerts.Accept(context.Background(), alert.ID, "h-winner", actor); err != nil {
		t.Fatalf("winner accept: %v", err)
	}
	_, _, err := svc.Alerts.Accept(context.Background(), alert.ID, "h-loser", orchestration.Actor{ID: "h-loser", Role: domain.RoleHandler})
	var taken *store.AlreadyAssignedError
	if !errors.As(err, &taken) || taken.AssigneeID != "h-winner" {
		t.Fatalf("loser: want AlreadyAssignedError by h-winner, got %v", err)
	}
}

// TestAlertResolve_RequiresActiveAssignee rejects a non-assignee resolver.
func TestAlertResolve_RequiresActiveAssignee(t *testing.T) {
	svc, _, st := newSvc(t)
	c := seedCollector(t, st, "R1")
	seedRule(t, st, "rule-r1", 8)
	rd := appendReading(t, st, c.ID, time.Date(2026, 8, 18, 12, 2, 0, 0, time.UTC), 20)
	alert := domain.Alert{ID: domain.AlertID("alert-r1"), CollectorID: c.ID, RuleID: "rule-r1",
		ReadingID: rd.ID, Severity: domain.SeverityWarn, State: domain.AlertStateOpen,
		FirstSeen: time.Now(), LastSeen: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = st.Alerts().Create(context.Background(), alert)
	owner := orchestration.Actor{ID: "h-owner", Role: domain.RoleHandler}
	if _, _, err := svc.Alerts.Accept(context.Background(), alert.ID, "h-owner", owner); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := svc.Alerts.Start(context.Background(), alert.ID, "h-owner", owner); err != nil {
		t.Fatalf("start: %v", err)
	}
	_, err := svc.Alerts.Resolve(context.Background(), alert.ID, "h-intruder", "nope", orchestration.Actor{ID: "h-intruder", Role: domain.RoleHandler})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("intruder resolve: want ErrValidation, got %v", err)
	}
}

// TestAlertRevoke_FromNonTerminal lets duty revoke an assigned alert.
func TestAlertRevoke_FromNonTerminal(t *testing.T) {
	svc, _, st := newSvc(t)
	c := seedCollector(t, st, "V1")
	seedRule(t, st, "rule-v1", 8)
	rd := appendReading(t, st, c.ID, time.Date(2026, 8, 18, 12, 3, 0, 0, time.UTC), 20)
	alert := domain.Alert{ID: domain.AlertID("alert-v1"), CollectorID: c.ID, RuleID: "rule-v1",
		ReadingID: rd.ID, Severity: domain.SeverityWarn, State: domain.AlertStateOpen,
		FirstSeen: time.Now(), LastSeen: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = st.Alerts().Create(context.Background(), alert)
	owner := orchestration.Actor{ID: "h-v", Role: domain.RoleHandler}
	if _, _, err := svc.Alerts.Accept(context.Background(), alert.ID, "h-v", owner); err != nil {
		t.Fatalf("accept: %v", err)
	}
	duty := orchestration.Actor{ID: "duty", Role: domain.RoleDuty}
	revoked, err := svc.Alerts.Revoke(context.Background(), alert.ID, duty)
	if err != nil || revoked.State != domain.AlertStateRevoked {
		t.Fatalf("revoke: %v state=%s", err, revoked.State)
	}
	assigns, _, _ := st.Assignments().List(context.Background(), store.AssignmentFilter{AlertID: alert.ID, Page: domain.Page{Size: 10}})
	if len(assigns) != 1 || assigns[0].State != domain.AssignmentCancelled {
		t.Fatalf("assignment not cancelled: %+v", assigns)
	}
}

// TestAlertBacklog_ReportsOverdue verifies the backlog surfaces overdue alerts.
func TestAlertBacklog_ReportsOverdue(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "B1")
	r := domain.Rule{ID: "rule-b1", Metric: domain.MetricQueueCount, MaxValue: ptrF(8), WindowSeconds: 60,
		Severity: domain.SeverityCritical, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = st.Rules().Create(context.Background(), r)
	rd := appendReading(t, st, c.ID, clock.Now(), 20)
	alert := domain.Alert{ID: domain.AlertID("alert-b1"), CollectorID: c.ID, RuleID: r.ID, ReadingID: rd.ID,
		Severity: domain.SeverityCritical, State: domain.AlertStateOpen, FirstSeen: clock.Now(),
		LastSeen: clock.Now(), CreatedAt: clock.Now(), UpdatedAt: clock.Now()}
	_ = st.Alerts().Create(context.Background(), alert)
	clock.Advance(30 * time.Minute)
	bl, err := svc.Alerts.Backlog(context.Background())
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if bl.OpenAlerts != 1 {
		t.Fatalf("open=%d", bl.OpenAlerts)
	}
	if len(bl.OverdueAlerts) != 1 {
		t.Fatalf("overdue=%d want 1", len(bl.OverdueAlerts))
	}
}

func ptrF(v float64) *float64 { return &v }
