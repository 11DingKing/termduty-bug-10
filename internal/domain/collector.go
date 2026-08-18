package domain

import "time"

// CollectorID uniquely identifies a service window or self-service terminal.
type CollectorID string

// CollectorKind distinguishes window counters from self-service terminals.
type CollectorKind string

const (
	CollectorKindWindow   CollectorKind = "window"
	CollectorKindTerminal CollectorKind = "terminal"
)

func (k CollectorKind) Valid() bool {
	return k == CollectorKindWindow || k == CollectorKindTerminal
}

// CollectorStatus records whether a collection point still feeds readings.
type CollectorStatus string

const (
	CollectorStatusActive   CollectorStatus = "active"
	CollectorStatusDisabled CollectorStatus = "disabled"
)

func (s CollectorStatus) Valid() bool {
	return s == CollectorStatusActive || s == CollectorStatusDisabled
}

// Collector is a monitored service window or self-service terminal. Each
// collector retains readings in its own ledger; the duty desk pulls them on a
// fixed cadence. A collector is also the unit of disablement and batch tuning.
type Collector struct {
	ID        CollectorID     `json:"id"`
	Code      string          `json:"code"`
	Name      string          `json:"name"`
	Kind      CollectorKind   `json:"kind"`
	Location  string          `json:"location"`
	Status    CollectorStatus `json:"status"`
	HandlerID string          `json:"handler_id"`
	Version   int64           `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// IsCollecting reports whether readings from this collector should be processed.
func (c Collector) IsCollecting() bool { return c.Status == CollectorStatusActive }

// Transition changes the collector status and returns the previous status, or
// returns ErrIllegalTransition when the change is not permitted.
func (c *Collector) Transition(to CollectorStatus) (CollectorStatus, error) {
	if !to.Valid() {
		return c.Status, ErrValidation
	}
	from := c.Status
	switch {
	case from == CollectorStatusActive && to == CollectorStatusDisabled:
	case from == CollectorStatusDisabled && to == CollectorStatusActive:
	default:
		return from, ErrIllegalTransition
	}
	c.Status = to
	return from, nil
}
