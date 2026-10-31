package models

import "time"

type TeamMemberRuntimeObservation struct {
	State      string     `json:"state"`
	Reason     string     `json:"reason,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

// Internal batch facts; these objects must not cross the northbound boundary.
type TeamMemberRuntimeFacts struct {
	Instance Instance
	Binding  *InstanceRuntimeBinding
	Runtime  *RuntimePod
}
