package models

import "time"

// Shared artifacts belong to the Team, not to a selected member instance.
type TeamWorkspaceFileAudit struct {
	ID           int64     `db:"id,primarykey,autoincrement"`
	TeamID       int       `db:"team_id"`
	UserID       int       `db:"user_id"`
	Action       string    `db:"action"`
	RelativePath string    `db:"relative_path"`
	Bytes        int64     `db:"bytes"`
	CreatedAt    time.Time `db:"created_at"`
}
