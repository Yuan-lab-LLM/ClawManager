package models

import "time"

const (
	EgressPrivateScopeInstance = "instance"
	EgressPrivateScopeUser     = "user"
)

// EgressPrivateException allows selected private CIDR:port targets through the egress proxy.
type EgressPrivateException struct {
	ID          int       `db:"id,primarykey,autoincrement" json:"id"`
	ScopeType   string    `db:"scope_type" json:"scope_type"`
	ScopeID     int       `db:"scope_id" json:"scope_id"`
	CIDR        string    `db:"cidr" json:"cidr"`
	Port        int       `db:"port" json:"port"`
	Enabled     bool      `db:"enabled" json:"enabled"`
	Description *string   `db:"description" json:"description,omitempty"`
	CreatedBy   *int      `db:"created_by" json:"created_by,omitempty"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// TableName returns the table name for egress private exceptions.
func (EgressPrivateException) TableName() string {
	return "egress_private_exceptions"
}
