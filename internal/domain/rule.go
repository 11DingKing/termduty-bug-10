package domain

import (
	"fmt"
	"time"
)

// Metric names the kind of reading value a rule guards.
type Metric string

const (
	MetricQueueCount Metric = "queue_count"
	MetricDuration   Metric = "duration_ms"
	MetricFaultCode  Metric = "fault_code"
)

func (m Metric) Valid() bool {
	switch m {
	case MetricQueueCount, MetricDuration, MetricFaultCode:
		return true
	}
	return false
}

// Severity classifies how urgently an alert must be handled.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityInfo, SeverityWarn, SeverityCritical:
		return true
	}
	return false
}

// Rule defines the agreed range for a metric on a collector. A reading outside
// the range for the configured window raises an alert; while the same object
// and problem remains unresolved, duplicate alerts are suppressed.
type Rule struct {
	ID            string       `json:"id"`
	CollectorID   *CollectorID `json:"collector_id"`
	Metric        Metric       `json:"metric"`
	WindowSeconds int          `json:"window_seconds"`
	MinValue      *float64     `json:"min_value"`
	MaxValue      *float64     `json:"max_value"`
	FaultTrigger  string       `json:"fault_trigger"`
	Severity      Severity     `json:"severity"`
	Enabled       bool         `json:"enabled"`
	Version       int64        `json:"version"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// AppliesTo reports whether the rule governs a given collector. A nil
// collector id means the rule is global and matches every collector.
func (r Rule) AppliesTo(id CollectorID) bool {
	if r.CollectorID == nil {
		return true
	}
	return *r.CollectorID == id
}

// Evaluate decides whether a reading breaches the rule and returns a human
// readable reason when it does.
func (r Rule) Evaluate(s ReadingSummary) (bool, string) {
	if !r.Enabled {
		return false, ""
	}
	switch r.Metric {
	case MetricQueueCount:
		v := float64(s.QueueCount)
		if r.MaxValue != nil && v > *r.MaxValue {
			return true, fmt.Sprintf("队列人数 %d 超过上限 %v", s.QueueCount, *r.MaxValue)
		}
		if r.MinValue != nil && v < *r.MinValue {
			return true, fmt.Sprintf("队列人数 %d 低于下限 %v", s.QueueCount, *r.MinValue)
		}
	case MetricDuration:
		v := float64(s.DurationMs)
		if r.MaxValue != nil && v > *r.MaxValue {
			return true, fmt.Sprintf("办件耗时 %dms 超过上限 %v", s.DurationMs, *r.MaxValue)
		}
		if r.MinValue != nil && v < *r.MinValue {
			return true, fmt.Sprintf("办件耗时 %dms 低于下限 %v", s.DurationMs, *r.MinValue)
		}
	case MetricFaultCode:
		if r.FaultTrigger != "" && s.FaultCode == r.FaultTrigger {
			return true, fmt.Sprintf("故障码 %s 命中触发条件", s.FaultCode)
		}
	}
	return false, ""
}
