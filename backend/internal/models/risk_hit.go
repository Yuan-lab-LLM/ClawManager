package models

import "time"

// RiskHit stores a normalized sensitive-data detection result.
type RiskHit struct {
	ID           int       `db:"id,primarykey,autoincrement" json:"id"`
	TraceID      string    `db:"trace_id" json:"trace_id"`
	SessionID    *string   `db:"session_id" json:"session_id,omitempty"`
	RequestID    *string   `db:"request_id" json:"request_id,omitempty"`
	UserID       *int      `db:"user_id" json:"user_id,omitempty"`
	InstanceID   *int      `db:"instance_id" json:"instance_id,omitempty"`
	InvocationID *int      `db:"invocation_id" json:"invocation_id,omitempty"`
	RuleID       string    `db:"rule_id" json:"rule_id"`
	RuleName     string    `db:"rule_name" json:"rule_name"`
	Severity     string    `db:"severity" json:"severity"`
	Action       string    `db:"action" json:"action"`
	MatchSummary string    `db:"match_summary" json:"match_summary"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// TableName returns the table name for risk hits.
func (r RiskHit) TableName() string {
	return "risk_hits"
}
