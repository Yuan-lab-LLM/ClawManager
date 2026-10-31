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

type RuntimeCatalogModel struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"context_window"`
	Reasoning     bool   `json:"reasoning"`
	Tools         bool   `json:"tools"`
	Vision        bool   `json:"vision"`
}
