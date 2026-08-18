package domain

import "time"

// AssignmentID uniquely identifies a dispatch / acceptance record.
type AssignmentID string

// AssignmentState records the lifecycle of a handler's claim on an alert.
type AssignmentState string

const (
	AssignmentActive    AssignmentState = "active"
	AssignmentCompleted AssignmentState = "completed"
	AssignmentCancelled AssignmentState = "cancelled"
)

func (s AssignmentState) Valid() bool {
	switch s {
	case AssignmentActive, AssignmentCompleted, AssignmentCancelled:
		return true
	}
	return false
}

// Assignment records that a responsible person accepted (or completed) an
// alert. At most one active assignment may exist per alert; concurrent
// accepters are resolved so that only the first one takes effect and the rest
// learn exactly who beat them to it.
type Assignment struct {
	ID          AssignmentID
	AlertID     AlertID
	HandlerID   string
	State       AssignmentState
	AcceptedAt  time.Time
	CompletedAt time.Time
	Note        string
	Version     int64
}

// IsActive reports whether the assignment currently holds the alert.
func (a Assignment) IsActive() bool { return a.State == AssignmentActive }
