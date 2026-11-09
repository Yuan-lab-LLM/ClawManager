package models

import (
	"fmt"
	"time"
)

// A durable, immutable stop target and historical process-group receipt.
// Confirmation is NOT proof that every workspace writer has exited.
type RuntimeUpgradeStop struct {
	InstanceID        int        `db:"instance_id"`
	OwnerID           int        `db:"owner_user_id"`
	Generation        int        `db:"generation"`
	Operation         string     `db:"operation_id"`
	RuntimePodID      int64      `db:"runtime_pod_id"`
	PodUID            string     `db:"pod_uid"`
	AgentEpoch        string     `db:"agent_epoch"`
	AgentEndpoint     string     `db:"agent_endpoint"`
	GatewayID         string     `db:"gateway_id"`
	PID               int        `db:"pid"`
	ConfirmationScope string     `db:"confirmation_scope"`
	ConfirmedAt       *time.Time `db:"confirmed_at"`
	CreatedAt         time.Time  `db:"created_at"`
}

func (s RuntimeUpgradeStop) ValidTarget() bool {
	return s.InstanceID > 0 && s.OwnerID > 0 && s.Generation >= 0 && ValidRuntimeOperation(s.Operation) &&
		s.RuntimePodID > 0 && s.PodUID != "" && len(s.PodUID) <= 128 && ValidRuntimeOperation(s.AgentEpoch) &&
		s.AgentEndpoint != "" && len(s.AgentEndpoint) <= 1024 && s.PID > 0 && s.GatewayID == fmt.Sprintf("gw-%d-%d", s.InstanceID, s.Generation)
}

func (s RuntimeUpgradeStop) SameTarget(other RuntimeUpgradeStop) bool {
	return s.InstanceID == other.InstanceID && s.OwnerID == other.OwnerID && s.Generation == other.Generation &&
		s.Operation == other.Operation && s.RuntimePodID == other.RuntimePodID && s.PodUID == other.PodUID &&
		s.AgentEpoch == other.AgentEpoch && s.AgentEndpoint == other.AgentEndpoint && s.GatewayID == other.GatewayID && s.PID == other.PID
}
