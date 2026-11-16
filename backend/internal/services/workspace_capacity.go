package services

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"time"
)

// WorkspaceCapacity reports proven ordinary free slots for a new placement.
// It never claims slots, starts/wakes runtimes, scales pools or writes audit state.
// Existing Agents can have additional instance-specific data/upgrade fences.
func (s *RuntimeScheduler) WorkspaceCapacity(ctx context.Context) (map[string]int, error) {
	if s == nil || s.podRepo == nil || s.deployments == nil {
		return nil, fmt.Errorf("runtime inventory unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result := map[string]int{}
	for _, kind := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		pods, err := s.podRepo.ListSchedulable(ctx, kind)
		if err != nil {
			return nil, err
		}
		// Test doubles can return a mixed pool; native SQL already applies this bound.
		matching := []models.RuntimePod{}
		for _, p := range pods {
			if p.RuntimeType == kind && !p.SchedulingPaused {
				matching = append(matching, p)
			}
		}
		pods, err = s.verifiedOrdinaryRuntimePods(ctx, kind, matching)
		if err != nil {
			return nil, err
		}
		if kind == RuntimeTypeOpenClaw && s.upgrade != nil {
			active, err := s.upgrade.rollouts.ListActive(ctx, kind)
			if err != nil {
				return nil, err
			}
			if len(active) > 0 {
				return nil, fmt.Errorf("upgrade placement temporarily unavailable")
			}

			prospectiveBirth := time.Now().UTC()
			pods, err = s.upgrade.filterUpgradePlacementAtBirth(ctx, 0, pods, nil, &prospectiveBirth)
			if err != nil {
				return nil, err
			}
		}
		free := 0
		for _, pod := range pods {
			canStart, err := s.podCanStartGateway(ctx, pod.ID)
			if err != nil {
				return nil, err
			}
			if !canStart {
				continue
			}
			n, err := s.inspectAllocatableGatewaySlots(ctx, pod, true)
			if err != nil {
				return nil, err
			}
			free += n
		}
		result[kind] = free
	}
	return result, nil
}
