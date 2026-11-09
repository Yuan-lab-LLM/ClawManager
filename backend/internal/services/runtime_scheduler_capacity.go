package services

import (
	"clawreef/internal/models"
	"context"
	"fmt"
)

// allocatableGatewaySlots uses the same physical reservations and SQL ownership
// as admission. Unknown inventory cannot justify expanding an incompatible pool.
func (s *RuntimeScheduler) allocatableGatewaySlots(ctx context.Context, pod models.RuntimePod) (int, error) {
	free := maxInt(0, pod.Capacity-pod.UsedSlots)
	inventory, ok := s.agentClient.(interface {
		reservedGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error)
	})
	if !ok || !requiresConfirmedGatewayStop(s.agentClient) {
		return free, nil
	}
	var reserved map[int]struct{}
	var err error
	if client, ok := s.agentClient.(*slotRuntimeAgentClient); ok {
		reserved, err = client.reservedGatewayPortsInspected(ctx, pod, true)
	} else {
		reserved, err = inventory.reservedGatewayPorts(ctx, pod)
	}
	if err != nil {
		return 0, err
	}
	bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
	if err != nil {
		return 0, err
	}
	var physical map[int]struct{}
	if layout, ok := s.agentClient.(interface {
		physicalGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error)
	}); ok {
		physical, err = layout.physicalGatewayPorts(ctx, pod)
		if err != nil {
			return 0, err
		}
		if physical == nil {
			return 0, fmt.Errorf("physical slot layout is unavailable")
		}
	}
	block := RuntimeGatewayPortBlockSize(pod.RuntimeType)
	if block < 1 {
		return 0, fmt.Errorf("invalid Runtime port block")
	}
	available, configured := 0, 0
	for port := s.gatewayPortStart; port+block-1 <= s.gatewayPortEndForPod(pod, pod.RuntimeType); port += block {
		configured++
		if physical != nil {
			if _, ok := physical[port]; !ok {
				return 0, fmt.Errorf("allocator port range differs from physical layout")
			}
		}
		if _, blocked := reserved[port]; !blocked && !runtimeGatewayPortBlockOccupied(bindings, port, block) {
			available++
		}
	}
	if physical != nil && len(physical) != configured {
		return 0, fmt.Errorf("allocator capacity differs from complete physical layout")
	}
	return minInt(free, available), nil
}
