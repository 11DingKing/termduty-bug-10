package domain

import "time"

// AlertID uniquely identifies an alert ticket.
type AlertID string

// AlertState is the lifecycle stage of an alert ticket.
type AlertState string

const (
	AlertStateOpen     AlertState = "open"
	AlertStateAssigned AlertState = "assigned"
	AlertStateHandling AlertState = "handling"
	AlertStateResolved AlertState = "resolved"
	AlertStateClosed   AlertState = "closed"
	AlertStateRevoked  AlertState = "revoked"
)

func (s AlertState) Valid() bool {
	switch s {
	case AlertStateOpen, AlertStateAssigned, AlertStateHandling,
		AlertStateResolved, AlertStateClosed, AlertStateRevoked:
		return true
	}
	return false
}

// Terminal reports whether the alert has reached a final, immutable state.
func (s AlertState) Terminal() bool {
	return s == AlertStateClosed || s == AlertStateRevoked
}

// Alert is a ticket raised when a reading crosses an agreed range and the
// underlying problem has not yet cleared. A single active assignment is the
// only one that can take effect on an alert.
type Alert struct {
	ID              AlertID
	CollectorID     CollectorID
	RuleID          string
	ReadingID       ReadingID
	Severity        Severity
	State           AlertState
	Message         string
	AssigneeID      string
	FirstSeen       time.Time
	LastSeen        time.Time
	SuppressedUntil time.Time
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CanNotify reports whether handlers may still be notified about this alert.
func (a Alert) CanNotify() bool {
	switch a.State {
	case AlertStateClosed, AlertStateRevoked, AlertStateResolved:
		return false
	}
	return true
}

// IsSuppressed reports whether duplicate notification is currently suppressed.
func (a Alert) IsSuppressed(now time.Time) bool {
	return !a.SuppressedUntil.IsZero() && now.Before(a.SuppressedUntil)
}
