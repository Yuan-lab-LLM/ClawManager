package models

import "time"

// ChatMessageRecord stores a persisted message within a chat session.
type ChatMessageRecord struct {
	ID           int       `db:"id,primarykey,autoincrement" json:"id"`
	TraceID      string    `db:"trace_id" json:"trace_id"`
	SessionID    string    `db:"session_id" json:"session_id"`
	RequestID    *string   `db:"request_id" json:"request_id,omitempty"`
	UserID       *int      `db:"user_id" json:"user_id,omitempty"`
	InstanceID   *int      `db:"instance_id" json:"instance_id,omitempty"`
	InvocationID *int      `db:"invocation_id" json:"invocation_id,omitempty"`
	Role         string    `db:"role" json:"role"`
	Content      string    `db:"content" json:"content"`
	SequenceNo   int       `db:"sequence_no" json:"sequence_no"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// TableName returns the table name for chat messages.
func (c ChatMessageRecord) TableName() string {
	return "chat_messages"
}
