package models

import "time"

// InstanceRuntimeRecovery is independent of instances.updated_at: runtime
// heartbeats must neither postpone recovery nor replenish its retry budget.
type InstanceRuntimeRecovery struct {
	Phase             string     `db:"phase" json:"phase,omitempty"`
	InstanceID        int        `db:"instance_id" json:"instance_id"`
	Attempts          int        `db:"attempts" json:"attempts"`
	NextRetryAt       *time.Time `db:"next_retry_at" json:"next_retry_at,omitempty"`
	Reason            string     `db:"reason" json:"reason,omitempty"`
	HealthySince      *time.Time `db:"healthy_since" json:"healthy_since,omitempty"`
	SourcePodID       int64      `db:"source_pod_id" json:"source_pod_id,omitempty"`
	SourceGatewayPort int        `db:"source_gateway_port" json:"source_gateway_port,omitempty"`
	Generation        int        `db:"generation" json:"generation"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updated_at"`
}
