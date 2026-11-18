package models

// GatewayTaskIdentity is scoped to the authenticated instance, not to caller-
// supplied user IDs. TaskID identifies one user turn, never an entire session.
type GatewayTaskIdentity struct {
	InstanceID int    `json:"-"`
	UserID     int    `json:"-"`
	Generation int    `json:"generation"`
	SessionID  string `json:"session_id"`
	TaskID     string `json:"task_id"`
}

type GatewayTaskState struct {
	Revoked             bool `json:"revoked" db:"revoked"`
	Finished            bool `json:"finished" db:"finished"`
	RuntimeAcknowledged bool `json:"runtime_acknowledged" db:"runtime_acknowledged"`
}

// A snapshot is evidence of server state only, not authorization to replay a
// receipt or infer that an old process/tool has stopped. Nil State means absent.
type GatewayTaskSnapshot struct {
	GatewayTaskIdentity
	State *GatewayTaskState `json:"state"`
}

type RuntimeCatalogModel struct {
	ID string `json:"id"`
	// Additive metadata; legacy runtimes keep using ID until they negotiate a
	// stable-identity catalog. Do not change the emitted ID during rollout.
	StableID string `json:"stable_id,omitempty"`
	// A same-name predecessor owns the legacy ref. New runtimes must use StableID.
	LegacyNameUnavailable bool `json:"legacy_name_unavailable,omitempty"`
	// Lookup-only names. Runtimes must not list these as additional deployments.
	Aliases          []string             `json:"aliases,omitempty"`
	ProviderRefs     []RuntimeProviderRef `json:"provider_refs,omitempty"`
	ContextWindow    int                  `json:"context_window"`
	Reasoning        bool                 `json:"reasoning"`
	ReasoningControl string               `json:"reasoning_control,omitempty"`
	Tools            bool                 `json:"tools"`
	Vision           bool                 `json:"vision"`
}
