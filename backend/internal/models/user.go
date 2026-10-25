package models

import (
	"time"
)

// User represents a user in the system
type User struct {
	ID           int        `db:"id,primarykey,autoincrement" json:"id"`
	Username     string     `db:"username" json:"username"`
	Email        string     `db:"email" json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"`
	Role         string     `db:"role" json:"role"`
	AuthProvider string     `db:"auth_provider" json:"auth_provider"`
	LoginAlias   *string    `db:"login_alias" json:"login_alias,omitempty"`
	ExternalID   *string    `db:"external_id" json:"external_id,omitempty"`
	IsActive     bool       `db:"is_active" json:"is_active"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
	LastLogin    *time.Time `db:"last_login" json:"last_login,omitempty"`
}

// TableName returns the table name for the User model
func (u User) TableName() string {
	return "users"
}
