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
	SchedulingPaused     bool       `db:"scheduling_paused" json:"scheduling_paused"`
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
	KubernetesPodUID              *string       `db:"-" json:"-"`
	RegisteredPodUID              *string       `db:"-" json:"-"`
	AgentState                    string        `db:"-" json:"-"`
	KubernetesReady               bool          `db:"-" json:"-"`
	CPUMillisLimit                *int64        `db:"-" json:"cpu_millis_limit,omitempty"`
	MemoryBytesLimit              *int64        `db:"-" json:"memory_bytes_limit,omitempty"`
	CPUMillisRequest              *int64        `db:"-" json:"-"`
	MemoryBytesRequest            *int64        `db:"-" json:"-"`
	CoordinatorCPUMillisRequest   *int64        `db:"-" json:"-"`
	CoordinatorMemoryBytesRequest *int64        `db:"-" json:"-"`
	StorageClass                  string        `db:"-" json:"storage_class,omitempty"`
	Slots                         []RuntimeSlot `db:"-" json:"slots"`
	WorkspaceStorageIdentity      string        `db:"-" json:"-"`
	CoordinatorCPUMillis          *int64        `db:"-" json:"-"`
	CoordinatorMemoryBytes        *int64        `db:"-" json:"-"`
}

// RuntimeSlot keeps Kubernetes container health separate from its assigned instance.
type RuntimeSlot struct {
	Index              int     `json:"index"`
	ContainerName      string  `json:"container_name"`
	Ready              bool    `json:"ready"`
	RestartCount       int32   `json:"restart_count"`
	Reason             string  `json:"reason,omitempty"`
	State              string  `json:"state"`
	CPUMillisUsed      *int64  `json:"cpu_millis_used,omitempty"`
	MemoryBytesUsed    *int64  `json:"memory_bytes_used,omitempty"`
	CPUMillisLimit     *int64  `json:"cpu_millis_limit,omitempty"`
	MemoryBytesLimit   *int64  `json:"memory_bytes_limit,omitempty"`
	CPUMillisRequest   *int64  `json:"-"`
	MemoryBytesRequest *int64  `json:"-"`
	InstanceID         *int    `json:"instance_id,omitempty"`
	InstanceName       string  `json:"instance_name,omitempty"`
	Owner              *string `json:"owner,omitempty"`
	InstanceState      string  `json:"instance_state,omitempty"`
	GatewayID          string  `json:"gateway_id,omitempty"`
	GatewayPort        *int    `json:"gateway_port,omitempty"`
	Generation         *int    `json:"generation,omitempty"`
	ErrorMessage       *string `json:"error_message,omitempty"`
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
