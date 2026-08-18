package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// AlertService raises alerts from readings, adjudicates the single-winner
// acceptance of a ticket, and drives the lifecycle transitions through the
// explicit state machine.
type AlertService struct {
	store *store.Store
	clock domain.Clock
	audit *AuditRecorder
	log   *slog.Logger
}

// Evaluate scans recent readings against enabled rules and raises or
// suppresses alerts. Suppression means the same object and problem is not
// notified twice while it remains unresolved.
func (s *AlertService) Evaluate(ctx context.Context, now time.Time) (EvalSummary, error) {
	summary := EvalSummary{}
	collectors, _, err := s.store.Collectors().List(ctx, store.CollectorFilter{Status: domain.CollectorStatusActive, Page: domain.Page{Size: 500}})
	if err != nil {
		return summary, err
	}
	summary.Collectors = len(collectors)
	for _, c := range collectors {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		rules, err := s.store.Rules().EnabledFor(ctx, c.ID)
		if err != nil {
			s.log.Warn("evaluate: rules load failed", "collector", c.ID, "err", err)
			continue
		}
		readings, err := s.store.Readings().Recent(ctx, c.ID, 200)
		if err != nil {
			s.log.Warn("evaluate: readings load failed", "collector", c.ID, "err", err)
			continue
		}
		for _, ru := range rules {
			summary.Evaluated++
			latest := latestWithin(readings, now, ru.WindowSeconds)
			if latest == nil {
				continue
			}
			breached, msg := ru.Evaluate(*latest)
			if !breached {
				continue
			}
			if existing, found, err := s.store.Alerts().FindActive(ctx, c.ID, ru.ID); err != nil {
				s.log.Warn("evaluate: find active failed", "collector", c.ID, "err", err)
				continue
			} else if found {
				summary.Suppressed++
				existing.LastSeen = now
				if err := s.store.Alerts().Update(ctx, &existing); err != nil {
					s.log.Warn("evaluate: suppress update failed", "alert", existing.ID, "err", err)
				}
				continue
			}
			alert := domain.Alert{
				ID:          domain.AlertID(newID()),
				CollectorID: c.ID,
				RuleID:      ru.ID,
				ReadingID:   latest.ID,
				Severity:    ru.Severity,
				State:       domain.AlertStateOpen,
				Message:     msg,
				FirstSeen:   now,
				LastSeen:    now,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := s.store.Alerts().Create(ctx, alert); err != nil {
				s.log.Warn("evaluate: create alert failed", "collector", c.ID, "err", err)
				continue
			}
			summary.Created++
			s.audit.bestEffort(ctx, "system", domain.RoleSystem, "alert.created", "alert", string(alert.ID), map[string]any{
				"collector": string(c.ID), "rule": ru.ID, "severity": string(ru.Severity), "message": msg,
			})
		}
	}
	return summary, nil
}

func latestWithin(readings []domain.ReadingSummary, now time.Time, windowSeconds int) *domain.ReadingSummary {
	if len(readings) == 0 {
		return nil
	}
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	threshold := now.Add(-time.Duration(windowSeconds) * time.Second)
	for i := range readings {
		if !readings[i].Timestamp.Before(threshold) {
			return &readings[i]
		}
	}
	return nil
}

// Accept adjudicates a single winner among concurrent accepters.
func (s *AlertService) Accept(ctx context.Context, alertID domain.AlertID, handlerID string, actor Actor) (domain.Assignment, domain.Alert, error) {
	assignment, alert, err := s.store.Assignments().AcceptAlert(ctx, alertID, handlerID)
	if err != nil {
		var taken *store.AlreadyAssignedError
		if errors.As(err, &taken) {
			s.audit.bestEffort(ctx, actor.ID, actor.Role, "accept.lost", "alert", string(alertID), map[string]any{
				"handler": handlerID, "winner": taken.AssigneeID,
			})
		}
		return domain.Assignment{}, domain.Alert{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "accept", "alert", string(alertID), map[string]any{
		"handler": handlerID, "assignment": string(assignment.ID),
	})
	return assignment, alert, nil
}

// Start moves an accepted alert into handling by its assignee.
func (s *AlertService) Start(ctx context.Context, alertID domain.AlertID, handlerID string, actor Actor) (domain.Alert, error) {
	return s.transition(ctx, alertID, domain.EventStart, handlerID, "", actor, "start")
}

// Resolve closes the handling loop with a receipt.
func (s *AlertService) Resolve(ctx context.Context, alertID domain.AlertID, handlerID, note string, actor Actor) (domain.Alert, error) {
	return s.transition(ctx, alertID, domain.EventResolve, handlerID, note, actor, "resolve")
}

// Release returns an alert to the open pool, cancelling the active dispatch.
func (s *AlertService) Release(ctx context.Context, alertID domain.AlertID, handlerID string, actor Actor) (domain.Alert, error) {
	return s.transition(ctx, alertID, domain.EventRelease, handlerID, "", actor, "release")
}

// Revoke lets the duty desk cancel a false alarm from any non-terminal state.
func (s *AlertService) Revoke(ctx context.Context, alertID domain.AlertID, actor Actor) (domain.Alert, error) {
	return s.transition(ctx, alertID, domain.EventRevoke, "", "", actor, "revoke")
}

// Close confirms a resolved alert, completing the receipt loop.
func (s *AlertService) Close(ctx context.Context, alertID domain.AlertID, actor Actor) (domain.Alert, error) {
	return s.transition(ctx, alertID, domain.EventClose, "", "", actor, "close")
}

func (s *AlertService) transition(ctx context.Context, alertID domain.AlertID, event domain.AlertEvent, handlerID, note string, actor Actor, action string) (domain.Alert, error) {
	alert, err := s.store.Alerts().Transition(ctx, alertID, event, handlerID, note)
	if err != nil {
		s.audit.bestEffort(ctx, actor.ID, actor.Role, action+".failed", "alert", string(alertID), map[string]any{
			"handler": handlerID, "error": err.Error(),
		})
		return domain.Alert{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, action, "alert", string(alertID), map[string]any{
		"handler": handlerID, "state": string(alert.State),
	})
	return alert, nil
}

// Get returns a single alert.
func (s *AlertService) Get(ctx context.Context, id domain.AlertID) (domain.Alert, error) {
	return s.store.Alerts().Get(ctx, id)
}

// List returns a page of alerts matching the filter.
func (s *AlertService) List(ctx context.Context, f store.AlertFilter) ([]domain.Alert, int64, error) {
	return s.store.Alerts().List(ctx, f)
}

// Backlog reports outstanding and overdue work for the duty desk.
func (s *AlertService) Backlog(ctx context.Context) (BacklogSummary, error) {
	summary := BacklogSummary{}
	counts, err := s.store.Alerts().CountByState(ctx)
	if err != nil {
		return summary, err
	}
	summary.OpenAlerts = counts[domain.AlertStateOpen]
	summary.AssignedAlerts = counts[domain.AlertStateAssigned]
	summary.HandlingAlerts = counts[domain.AlertStateHandling]
	now := s.clock.Now()
	for _, state := range []domain.AlertState{domain.AlertStateOpen, domain.AlertStateAssigned, domain.AlertStateHandling} {
		items, _, err := s.store.Alerts().List(ctx, store.AlertFilter{State: state, Page: domain.Page{Size: 200}})
		if err != nil {
			return summary, err
		}
		for _, a := range items {
			if a.CreatedAt.Add(slaFor(a.Severity)).Before(now) {
				summary.OverdueAlerts = append(summary.OverdueAlerts, a)
			}
		}
	}
	summary.PendingIngest, _ = s.store.Ingest().PendingCount(ctx)
	summary.LeasedIngest, _ = s.store.Ingest().LeasedCount(ctx)
	failures, _, err := s.store.Failures().List(ctx, store.FailureFilter{Resolved: boolPtr(false), Page: domain.Page{Size: 50}})
	if err != nil {
		return summary, err
	}
	summary.Failures = failures
	summary.DeadLettered = len(failures)
	return summary, nil
}

func slaFor(sev domain.Severity) time.Duration {
	switch sev {
	case domain.SeverityCritical:
		return 15 * time.Minute
	case domain.SeverityWarn:
		return time.Hour
	default:
		return 2 * time.Hour
	}
}
