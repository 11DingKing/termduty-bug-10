package store_test

import (
	"context"
	"errors"
	"testing"

	"termduty/internal/domain"
)

func TestStartRequiresTheActiveAssignee(t *testing.T) {
	st, clock := newTestStore(t)
	c := mustCollector(t, st, "SA1")
	alert := domain.Alert{ID: "alert-start-owner", CollectorID: c.ID, RuleID: "rule", Severity: domain.SeverityWarn,
		State: domain.AlertStateOpen, CreatedAt: clock.Now(), FirstSeen: clock.Now(), LastSeen: clock.Now(), UpdatedAt: clock.Now()}
	if err := st.Alerts().Create(context.Background(), alert); err != nil { t.Fatalf("create alert: %v", err) }
	if _, _, err := st.Assignments().AcceptAlert(context.Background(), alert.ID, "handler-owner"); err != nil { t.Fatalf("accept: %v", err) }
	_, err := st.Alerts().Transition(context.Background(), alert.ID, domain.EventStart, "handler-intruder", "")
	if !errors.Is(err, domain.ErrValidation) { t.Fatalf("intruder start err=%v want validation", err) }
}
