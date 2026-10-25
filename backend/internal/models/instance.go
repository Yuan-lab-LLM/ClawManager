package models

import (
	"time"
)

// Instance represents a virtual desktop instance
type Instance struct {
	ID                       int        `db:"id,primarykey,autoincrement" json:"id"`
	LastOnlineAt             *time.Time `db:"-" json:"last_online_at,omitempty"`
	UserID                   int        `db:"user_id" json:"user_id"`
	Owner                    *string    `db:"owner" json:"owner,omitempty"`
	Name                     string     `db:"name" json:"name"`
	Alias                    *string    `db:"alias" json:"alias,omitempty"`
	Description              *string    `db:"description" json:"description,omitempty"`
	Type                     string     `db:"type" json:"type"`
	RuntimeType              string     `db:"runtime_type" json:"runtime_type"`
	RuntimeVariant           string     `db:"runtime_variant" json:"runtime_variant,omitempty"`
	InstanceMode             string     `db:"instance_mode" json:"instance_mode"`
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
	DesktopStreamProfile     string     `db:"-" json:"desktop_stream_profile,omitempty"`
	StorageClass             string     `db:"storage_class" json:"storage_class"`
	PVCName                  *string    `db:"pvc_name" json:"-"`
	MountPath                string     `db:"mount_path" json:"mount_path"`
	WorkspacePath            *string    `db:"workspace_path" json:"workspace_path,omitempty"`
	WorkspaceUsageBytes      int64      `db:"workspace_usage_bytes" json:"workspace_usage_bytes"`
	RuntimeGeneration        int        `db:"runtime_generation" json:"runtime_generation"`
	RuntimeErrorMessage      *string    `db:"runtime_error_message" json:"runtime_error_message,omitempty"`
	RuntimeErrorCode         *string    `db:"runtime_error_code" json:"runtime_error_code,omitempty"`
	ProvisioningOperationID  *string    `db:"provisioning_operation_id" json:"-"`
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

// InstanceListFilter contains the caller-scoped filters supported by the
// workspace instance list. Empty fields do not restrict the query.
type InstanceListFilter struct {
	Query        string
	Type         string
	InstanceMode string
	Availability string
	Status       string
}

// InstanceSummary is the aggregate instance data used by the user dashboard.
// AllocatedStorageGB reflects configured instance capacity, not measured
// workspace filesystem usage.
type InstanceSummary struct {
	Total              int   `json:"total"`
	Running            int   `json:"running"`
	Creating           int   `json:"creating"`
	Stopped            int   `json:"stopped"`
	Error              int   `json:"error"`
	Deleting           int   `json:"deleting"`
	AllocatedStorageGB int64 `json:"allocated_storage_gb"`
}

// TableName returns the table name for the Instance model
func (i Instance) TableName() string {
	return "instances"
}
