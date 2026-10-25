package models

import (
	"encoding/json"
	"time"
)

type RuntimePod struct {
	ID                   int64      `db:"id,primarykey,autoincrement" json:"id"`
	RuntimeType          string     `db:"runtime_type" json:"runtime_type"`
	Namespace            string     `db:"namespace" json:"namespace"`
	PodName              string     `db:"pod_name" json:"pod_name"`
	PodUID               *string    `db:"pod_uid" json:"pod_uid,omitempty"`
	PodIP                *string    `db:"pod_ip" json:"pod_ip,omitempty"`
	NodeName             *string    `db:"node_name" json:"node_name,omitempty"`
	DeploymentName       string     `db:"deployment_name" json:"deployment_name"`
	ImageRef             string     `db:"image_ref" json:"image_ref"`
	OpenClawVersion      *string    `db:"openclaw_version" json:"openclaw_version,omitempty"`
	AgentProtocolVersion *string    `db:"agent_protocol_version" json:"agent_protocol_version,omitempty"`
	TeamPluginVersion    *string    `db:"team_plugin_version" json:"team_plugin_version,omitempty"`
	SessionStore         *string    `db:"session_store" json:"session_store,omitempty"`
	ImageDigest          *string    `db:"image_digest" json:"image_digest,omitempty"`
	CapabilitiesJSON     *string    `db:"capabilities_json" json:"-"`
	AgentEndpoint        *string    `db:"agent_endpoint" json:"agent_endpoint,omitempty"`
	State                string     `db:"state" json:"state"`
	Capacity             int        `db:"capacity" json:"capacity"`
	UsedSlots            int        `db:"used_slots" json:"used_slots"`
	Draining             bool       `db:"draining" json:"draining"`
	CPUMillisUsed        int64      `db:"cpu_millis_used" json:"cpu_millis_used"`
	MemoryBytesUsed      int64      `db:"memory_bytes_used" json:"memory_bytes_used"`
	DiskBytesUsed        int64      `db:"disk_bytes_used" json:"disk_bytes_used"`
	NetworkRXBytes       int64      `db:"network_rx_bytes" json:"network_rx_bytes"`
	NetworkTXBytes       int64      `db:"network_tx_bytes" json:"network_tx_bytes"`
	MetricsJSON          *string    `db:"metrics_json" json:"-"`
	LastSeenAt           *time.Time `db:"last_seen_at" json:"last_seen_at,omitempty"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt            time.Time  `db:"updated_at" json:"updated_at"`
	// Pool metadata is discovered from the owning Kubernetes Deployment. It is
	// deliberately transient: older Runtime Agents and existing database rows do
	// not need a schema change, while control-plane callers can still distinguish
	// serving, upgrade and isolated-lab pools.
	PoolRole          string `db:"-" json:"pool_role,omitempty"`
	PoolPurpose       string `db:"-" json:"pool_purpose,omitempty"`
	UpgradeID         string `db:"-" json:"upgrade_id,omitempty"`
	SourceDeployment  string `db:"-" json:"source_deployment,omitempty"`
	SchedulingEnabled *bool  `db:"-" json:"scheduling_enabled,omitempty"`
	DesiredReplicas   int32  `db:"-" json:"desired_replicas,omitempty"`
	// Only populated by the control-plane Kubernetes inventory, never by an
	// Agent report. Legacy Agents may omit pod_uid in their registration.
	KubernetesPodUID *string `db:"-" json:"-"`
	KubernetesReady  bool    `db:"-" json:"-"`
}

func (p RuntimePod) Capabilities() []string {
	if p.CapabilitiesJSON == nil || *p.CapabilitiesJSON == "" {
		return nil
	}
	var capabilities []string
	_ = json.Unmarshal([]byte(*p.CapabilitiesJSON), &capabilities)
	return capabilities
}

func (RuntimePod) TableName() string {
	return "runtime_pods"
}
