package models

import (
	"strings"
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

// DefaultQuotaForRole returns the platform defaults for a role. Keep these
// values in one place so user creation, imports, and role synchronization use
// the same defaults.
func DefaultQuotaForRole(role string) UserQuota {
	if strings.EqualFold(strings.TrimSpace(role), "admin") {
		return UserQuota{
			MaxInstances: 100,
			MaxCPUCores:  200,
			MaxMemoryGB:  1000,
			MaxStorageGB: 5000,
			MaxGPUCount:  10,
		}
	}

	return UserQuota{
		MaxInstances: 10,
		MaxCPUCores:  40,
		MaxMemoryGB:  100,
		MaxStorageGB: 500,
		MaxGPUCount:  2,
	}
}

// IsDefaultForRole compares only resource values, not database metadata.
func (u UserQuota) IsDefaultForRole(role string) bool {
	defaults := DefaultQuotaForRole(role)
	return u.MaxInstances == defaults.MaxInstances &&
		u.MaxCPUCores == defaults.MaxCPUCores &&
		u.MaxMemoryGB == defaults.MaxMemoryGB &&
		u.MaxStorageGB == defaults.MaxStorageGB &&
		u.MaxGPUCount == defaults.MaxGPUCount
}

// ApplyResourceValues copies resource values while preserving quota identity.
func (u *UserQuota) ApplyResourceValues(source UserQuota) {
	u.MaxInstances = source.MaxInstances
	u.MaxCPUCores = source.MaxCPUCores
	u.MaxMemoryGB = source.MaxMemoryGB
	u.MaxStorageGB = source.MaxStorageGB
	u.MaxGPUCount = source.MaxGPUCount
}
