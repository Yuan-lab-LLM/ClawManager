package models

import "time"

type CustomTeamTemplate struct {
	LastOperationID      *string   `db:"last_operation_id" json:"-"`
	ResourceScope        string    `db:"resource_scope" json:"-"`
	Owner                *string   `db:"owner" json:"-"`
	ID                   int       `db:"id,primarykey,autoincrement" json:"id"`
	UserID               int       `db:"user_id" json:"user_id"`
	Name                 string    `db:"name" json:"name"`
	Intent               string    `db:"intent" json:"intent"`
	RequestedMemberCount *int      `db:"requested_member_count" json:"requested_member_count,omitempty"`
	ResolvedMemberCount  int       `db:"resolved_member_count" json:"resolved_member_count"`
	SpecJSON             string    `db:"spec_json" json:"-"`
	Revision             int       `db:"revision" json:"revision"`
	LastTraceID          *string   `db:"last_trace_id" json:"-"`
	CreatedAt            time.Time `db:"created_at" json:"created_at"`
	UpdatedAt            time.Time `db:"updated_at" json:"updated_at"`
}

func (CustomTeamTemplate) TableName() string {
	return "custom_team_templates"
}
