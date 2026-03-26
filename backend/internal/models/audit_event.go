package models

import "time"

// AuditEvent stores normalized AI governance events.
type AuditEvent struct {
	ID           int       `db:"id,primarykey,autoincrement" json:"id"`
	TraceID      string    `db:"trace_id" json:"trace_id"`
	SessionID    *string   `db:"session_id" json:"session_id,omitempty"`
	RequestID    *string   `db:"request_id" json:"request_id,omitempty"`
	UserID       *int      `db:"user_id" json:"user_id,omitempty"`
	InstanceID   *int      `db:"instance_id" json:"instance_id,omitempty"`
	InvocationID *int      `db:"invocation_id" json:"invocation_id,omitempty"`
	EventType    string    `db:"event_type" json:"event_type"`
	TrafficClass string    `db:"traffic_class" json:"traffic_class"`
	Severity     string    `db:"severity" json:"severity"`
	Message      string    `db:"message" json:"message"`
	Details      *string   `db:"details" json:"details,omitempty"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// TableName returns the table name for audit events.
func (a AuditEvent) TableName() string {
	return "audit_events"
}
