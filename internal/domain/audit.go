package domain

import "time"

// Role identifies the actor class that performed an audited action. The duty
// desk can only observe and judge, responsible persons can only handle, and
// center operations can bulk-tune agreed ranges or disable collection points.
type Role string

const (
	RoleDuty    Role = "duty"
	RoleHandler Role = "handler"
	RoleOps     Role = "ops"
	RoleSystem  Role = "system"
)

func (r Role) Valid() bool {
	switch r {
	case RoleDuty, RoleHandler, RoleOps, RoleSystem:
		return true
	}
	return false
}

// AuditEntry is an immutable record of who did what, when, against which
// business object. Entries are queryable by target and by actor.
type AuditEntry struct {
	ID         string         `json:"id"`
	Actor      string         `json:"actor"`
	Role       Role           `json:"role"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Detail     map[string]any `json:"detail"`
	At         time.Time      `json:"at"`
}
