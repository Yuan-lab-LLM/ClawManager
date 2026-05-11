package models

import (
	"time"
)

// Instance represents a virtual desktop instance
type Instance struct {
	ID                       int        `db:"id,primarykey,autoincrement" json:"id"`
	UserID                   int        `db:"user_id" json:"user_id"`
	Name                     string     `db:"name" json:"name"`
	Description              *string    `db:"description" json:"description,omitempty"`
	Type                     string     `db:"type" json:"type"`
	Status                   string     `db:"status" json:"status"`
	CPUCores                 float64    `db:"cpu_cores" json:"cpu_cores"`
	MemoryGB                 int        `db:"memory_gb" json:"memory_gb"`
	DiskGB                   int        `db:"disk_gb" json:"disk_gb"`
	GPUEnabled               bool       `db:"gpu_enabled" json:"gpu_enabled"`
	GPUType                  *string    `db:"gpu_type" json:"gpu_type,omitempty"`
	GPUCount                 int        `db:"gpu_count" json:"gpu_count"`
	OSType                   string     `db:"os_type" json:"os_type"`
	OSVersion                string     `db:"os_version" json:"os_version"`
	ImageRegistry            *string    `db:"image_registry" json:"image_registry,omitempty"`
	ImageTag                 *string    `db:"image_tag" json:"image_tag,omitempty"`
	EnvironmentOverridesJSON *string    `db:"environment_overrides_json" json:"-"`
	StorageClass             string     `db:"storage_class" json:"storage_class"`
	MountPath                string     `db:"mount_path" json:"mount_path"`
	PodName                  *string    `db:"pod_name" json:"pod_name,omitempty"`
	PodNamespace             *string    `db:"pod_namespace" json:"pod_namespace,omitempty"`
	PodIP                    *string    `db:"pod_ip" json:"pod_ip,omitempty"`
	AccessURL                *string    `db:"access_url" json:"access_url,omitempty"`
	AccessToken              *string    `db:"access_token" json:"-"`
	AgentBootstrapToken      *string    `db:"agent_bootstrap_token" json:"-"`
	OpenClawConfigSnapshotID *int       `db:"openclaw_config_snapshot_id" json:"openclaw_config_snapshot_id,omitempty"`
	CreatedAt                time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at" json:"updated_at"`
	StartedAt                *time.Time `db:"started_at" json:"started_at,omitempty"`
	StoppedAt                *time.Time `db:"stopped_at" json:"stopped_at,omitempty"`
}

// TableName returns the table name for the Instance model
func (i Instance) TableName() string {
	return "instances"
}
