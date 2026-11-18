package models

import "time"

// RuntimeFailoverIntent records control-plane stop authority independently of
// mutable Pod registration and heartbeat health. It is never created by reports.
type RuntimeFailoverIntent struct {
	InstanceID     int        `db:"instance_id" json:"instance_id"`
	UserID         int        `db:"user_id" json:"user_id"`
	Generation     int        `db:"generation" json:"generation"`
	RuntimeType    string     `db:"runtime_type" json:"runtime_type"`
	SourcePodID    int64      `db:"source_pod_id" json:"source_pod_id"`
	SourcePodUID   string     `db:"source_pod_uid" json:"source_pod_uid"`
	SourceEndpoint string     `db:"source_endpoint" json:"source_endpoint"`
	GatewayID      string     `db:"gateway_id" json:"gateway_id"`
	GatewayPort    int        `db:"gateway_port" json:"gateway_port"`
	WorkspacePath  string     `db:"workspace_path" json:"workspace_path"`
	Reason         string     `db:"reason" json:"reason"`
	Attempts       int        `db:"attempts" json:"attempts"`
	NextRetryAt    *time.Time `db:"next_retry_at" json:"next_retry_at,omitempty"`
	SourceStopped  bool       `db:"source_stopped" json:"source_stopped"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
}
