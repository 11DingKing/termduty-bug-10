package domain

import "fmt"

// AlertEvent enumerates the lifecycle events that drive alert state changes.
type AlertEvent string

const (
	EventAccept  AlertEvent = "accept"
	EventStart   AlertEvent = "start"
	EventResolve AlertEvent = "resolve"
	EventClose   AlertEvent = "close"
	EventRevoke  AlertEvent = "revoke"
	EventRelease AlertEvent = "release"
)

// alertTransition is one legal entry in the explicit alert state table.
type alertTransition struct {
	from  AlertState
	event AlertEvent
	to    AlertState
}

// alertTable is the explicit alert state transition table. Any change not
// listed here is rejected as an illegal transition, so a misrouted handler can
// never silently put an alert back into a state it already left.
var alertTable = []alertTransition{
	{AlertStateOpen, EventAccept, AlertStateAssigned},
	{AlertStateAssigned, EventStart, AlertStateHandling},
	{AlertStateAssigned, EventRelease, AlertStateOpen},
	{AlertStateHandling, EventResolve, AlertStateResolved},
	{AlertStateHandling, EventRelease, AlertStateOpen},
	{AlertStateResolved, EventClose, AlertStateClosed},
	{AlertStateOpen, EventRevoke, AlertStateRevoked},
	{AlertStateAssigned, EventRevoke, AlertStateRevoked},
	{AlertStateHandling, EventRevoke, AlertStateRevoked},
}

// NextState resolves an event against the current alert state. It returns the
// resulting state, or an error wrapping ErrIllegalTransition when the move is
// not permitted by the table.
func NextState(from AlertState, event AlertEvent) (AlertState, error) {
	for _, t := range alertTable {
		if t.from == from && t.event == event {
			return t.to, nil
		}
	}
	return from, fmt.Errorf("%w: alert %s on %s", ErrIllegalTransition, event, from)
}

// LegalTransitions returns the events permitted from a given state, useful for
// rendering available actions in the UI and asserting invariants in tests.
func LegalTransitions(from AlertState) []AlertEvent {
	out := make([]AlertEvent, 0, 3)
	for _, t := range alertTable {
		if t.from == from {
			out = append(out, t.event)
		}
	}
	return out
}
