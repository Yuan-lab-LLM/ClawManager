package services

import (
	"context"
	"encoding/json"
	"fmt"

	"clawreef/internal/models"
)

func (c *slotRuntimeAgentClient) reservedGatewayPorts(ctx context.Context, pod models.RuntimePod) (map[int]struct{}, error) {
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
	return ports, nil
}
