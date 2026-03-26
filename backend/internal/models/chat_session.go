package models

import "time"

// ChatSession stores a persisted AI chat session.
type ChatSession struct {
	ID             int       `db:"id,primarykey,autoincrement" json:"id"`
	SessionID      string    `db:"session_id" json:"session_id"`
	UserID         *int      `db:"user_id" json:"user_id,omitempty"`
	InstanceID     *int      `db:"instance_id" json:"instance_id,omitempty"`
	Title          *string   `db:"title" json:"title,omitempty"`
	LastTraceID    *string   `db:"last_trace_id" json:"last_trace_id,omitempty"`
	StartedAt      time.Time `db:"started_at" json:"started_at"`
	LastActivityAt time.Time `db:"last_activity_at" json:"last_activity_at"`
}

// TableName returns the table name for chat sessions.
func (c ChatSession) TableName() string {
	return "chat_sessions"
}
