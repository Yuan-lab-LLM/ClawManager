package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"clawreef/internal/models"
)

func (c *slotRuntimeAgentClient) reservedGatewayPorts(ctx context.Context, pod models.RuntimePod) (map[int]struct{}, error) {
	return c.reservedGatewayPortsInspected(ctx, pod, false)
}

func (c *slotRuntimeAgentClient) reservedGatewayPortsInspected(ctx context.Context, pod models.RuntimePod, requireCapacityProof bool) (map[int]struct{}, error) {
	if pod.AgentEndpoint == nil || pod.PodUID == nil || pod.Capacity < 1 {
		return nil, fmt.Errorf("registered slot target identity is required")
	}
	uid, err := c.targetPodUID(ctx, *pod.AgentEndpoint)
	if err != nil || uid != *pod.PodUID {
		return nil, fmt.Errorf("slot reservation target Pod identity changed")
	}
	assignments, err := c.assignments.ReservedAssignments(ctx, *pod.AgentEndpoint, uid, pod.RuntimeType)
	if err != nil {
		return nil, err
	}
	ports := make(map[int]struct{}, len(assignments))
	for _, a := range assignments {
		var req RuntimeAgentCreateGatewayRequest
		if json.Unmarshal(a.Request, &req) != nil || req.InstanceID != a.InstanceID || req.UserID != a.UserID || req.Generation != a.Generation || req.PodUID != uid || req.AgentType != a.RuntimeType || req.GatewayPort < 1 || req.GatewayPort > 65535 {
			return nil, fmt.Errorf("persisted slot request does not match assignment identity")
		}
		ports[req.GatewayPort] = struct{}{}
	}

	inspectionCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	prior, quarantine, err := c.readSlotAudit(inspectionCtx, pod)
	if err != nil {
		return nil, err
	}
	observations, err := c.observeRuntimeSlots(inspectionCtx, pod)
	if err != nil {
		return nil, err
	}
	// A later authenticated full-exit proof may resolve an old quarantine without
	// waiting for the periodic cursor. Unknown ports and incomplete proofs retain
	// their exclusions; this grants no authority to stop owners or release bindings.
	changed, complete := false, true
	for _, observation := range observations {
		if observation.Err != nil {
			if requireCapacityProof {
				// Unknown/legacy proof reserves the port defensively, but cannot
				// justify expanding another replica of the same pool.
				return nil, fmt.Errorf("runtime capacity proof unavailable: %w", observation.Err)
			}
			complete = false
		}
		if observation.free() {
			if _, exists := quarantine.Ports[observation.Port]; exists {
				delete(quarantine.Ports, observation.Port)
				changed = true
			}
		}
	}
	if complete && quarantine.PodError != "" {
		quarantine.PodError = ""
		changed = true
	}
	if prior != nil && changed {
		quarantine.InspectedAt = time.Now().UTC()
		// Do not retry stale evidence after a CAS conflict: newer quarantine wins.
		if err := c.writeSlotAudit(inspectionCtx, pod, prior, quarantine); err != nil {
			return nil, err
		}
	}
	if quarantine.PodError != "" {
		return nil, fmt.Errorf("runtime slot audit quarantine: %s", quarantine.PodError)
	}
	for port := range quarantine.Ports {
		ports[port] = struct{}{}
	}
	for _, observation := range observations {
		if !observation.free() {
			ports[observation.Port] = struct{}{}
		}
	}
	return ports, nil
}
