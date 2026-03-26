package models

import "time"

// RiskRule stores an admin-managed sensitive detection rule.
type RiskRule struct {
	ID          int       `db:"id,primarykey,autoincrement" json:"id"`
	RuleID      string    `db:"rule_id" json:"rule_id"`
	DisplayName string    `db:"display_name" json:"display_name"`
	Description *string   `db:"description" json:"description,omitempty"`
	Pattern     string    `db:"pattern" json:"pattern"`
	Severity    string    `db:"severity" json:"severity"`
	Action      string    `db:"action" json:"action"`
	IsEnabled   bool      `db:"is_enabled" json:"is_enabled"`
	SortOrder   int       `db:"sort_order" json:"sort_order"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// TableName returns the table name for risk rules.
func (r RiskRule) TableName() string {
	return "risk_rules"
}
