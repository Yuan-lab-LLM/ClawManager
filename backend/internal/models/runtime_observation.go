package models

import "time"

// RuntimePodObservation contains only sanitized facts from a scan of one Pod UID.
// Reported identity is evidence of occupancy, never binding or cleanup authority.
type RuntimePodObservation struct {
	PodUID     string                   `json:"pod_uid"`
	ObservedAt time.Time                `json:"observed_at"`
	Slots      []RuntimeSlotObservation `json:"slots"`
	Error      string                   `json:"error,omitempty"`
}

type RuntimeSlotObservation struct {
	ContainerName    string `json:"container_name"`
	GatewayPort      int    `json:"gateway_port"`
	State            string `json:"state"`
	InstanceID       int    `json:"instance_id"`
	UserID           int    `json:"user_id"`
	GatewayID        string `json:"gateway_id"`
	Generation       int    `json:"generation"`
	InstanceState    string `json:"instance_state"`
	GatewayState     string `json:"gateway_state"`
	IdentityVerified bool   `json:"identity_verified"`
	Error            string `json:"error,omitempty"`
}
