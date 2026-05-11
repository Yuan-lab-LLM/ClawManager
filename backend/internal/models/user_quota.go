package models

import (
	"time"
)

// UserQuota represents resource quota for a user
type UserQuota struct {
	ID           int       `db:"id,primarykey,autoincrement" json:"id"`
	UserID       int       `db:"user_id" json:"user_id"`
	MaxInstances int       `db:"max_instances" json:"max_instances"`
	MaxCPUCores  float64   `db:"max_cpu_cores" json:"max_cpu_cores"`
	MaxMemoryGB  int       `db:"max_memory_gb" json:"max_memory_gb"`
	MaxStorageGB int       `db:"max_storage_gb" json:"max_storage_gb"`
	MaxGPUCount  int       `db:"max_gpu_count" json:"max_gpu_count"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// TableName returns the table name for the UserQuota model
func (u UserQuota) TableName() string {
	return "user_quotas"
}
