package models

import "time"

type SkillPackageMaterializeJob struct {
	ID             int        `db:"id,primarykey,autoincrement" json:"id"`
	InstanceID     int        `db:"instance_id" json:"instance_id"`
	SkillID        int        `db:"skill_id" json:"skill_id"`
	BlobID         int        `db:"blob_id" json:"blob_id"`
	WorkspaceDir   string     `db:"workspace_dir" json:"workspace_dir"`
	ContentHash    string     `db:"content_hash" json:"content_hash"`
	Status         string     `db:"status" json:"status"`
	AttemptCount   int        `db:"attempt_count" json:"attempt_count"`
	MaxAttempts    int        `db:"max_attempts" json:"max_attempts"`
	LastError      *string    `db:"last_error" json:"last_error,omitempty"`
	IdempotencyKey string     `db:"idempotency_key" json:"idempotency_key"`
	TriggerSource  string     `db:"trigger_source" json:"trigger_source"`
	StartedAt      *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt     *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
}

func (j SkillPackageMaterializeJob) TableName() string {
	return "skill_package_materialize_jobs"
}
