package models

import "time"

type TeamRuntimeState struct {
	State        string     `json:"state"`
	OperationID  string     `json:"operation_id,omitempty"`
	DesiredState string     `json:"desired_state"`
	Reason       string     `json:"reason,omitempty"`
	Progress     int        `json:"progress"`
	Total        int        `json:"total"`
	Error        string     `json:"error,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	StoppedAt    *time.Time `json:"stopped_at,omitempty"`
	CanStart     bool       `json:"can_start"`
	CanStop      bool       `json:"can_stop"`
}

// Frozen by the server; never accepted from model or browser input.
type TeamLifecycleMember struct {
	InstanceID          int        `json:"instance_id"`
	MemberID            int        `json:"member_id"`
	Generation          int        `json:"generation"`
	Role                string     `json:"role"`
	Workspace           string     `json:"workspace"`
	ColdStopOperationID string     `json:"cold_stop_operation_id,omitempty"`
	ColdStoppedAt       *time.Time `json:"cold_stopped_at,omitempty"`
}

type TeamLifecycleOperation struct {
	TeamRuntimeState
	TeamID   int
	Members  []TeamLifecycleMember
	Attempts int
}
