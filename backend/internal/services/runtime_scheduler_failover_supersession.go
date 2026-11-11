package services

import (
	"context"
	"encoding/json"
	"fmt"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// A generation counter alone cannot retire uncertain stop authority. Require a
// complete current SQL binding and the native controller's frozen publication
// for that successor, which is subject to the normal predecessor-stop fences.
func (s *RuntimeScheduler) retireSupersededRuntimeFailoverLocked(ctx context.Context, store repository.RuntimeFailoverRepository, prior models.RuntimeFailoverIntent, instance models.Instance, binding *models.InstanceRuntimeBinding) (bool, error) {
	if instance.UserID != prior.UserID || instance.RuntimeGeneration <= prior.Generation || instance.Type != prior.RuntimeType || instance.RuntimeType != RuntimeBackendGateway || instance.InstanceMode != InstanceModeLite || stringValue(instance.WorkspacePath) != prior.WorkspacePath || binding == nil || binding.InstanceID != instance.ID || binding.Generation != instance.RuntimeGeneration || binding.RuntimeType != instance.Type || binding.WorkspacePath != prior.WorkspacePath || binding.GatewayID != fmt.Sprintf("gw-%d-%d", instance.ID, instance.RuntimeGeneration) {
		return false, fmt.Errorf("older runtime failover has no authoritative successor binding")
	}
	pod, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return false, err
	}
	if pod == nil || stringValue(pod.PodUID) == "" || stringValue(pod.AgentEndpoint) == "" || pod.RuntimeType != instance.Type {
		return false, fmt.Errorf("runtime failover successor Pod identity is unavailable")
	}
	successor := models.RuntimeFailoverIntent{InstanceID: instance.ID, UserID: instance.UserID, Generation: instance.RuntimeGeneration, RuntimeType: instance.Type, SourcePodID: pod.ID, SourcePodUID: *pod.PodUID, SourceEndpoint: *pod.AgentEndpoint, GatewayID: binding.GatewayID, GatewayPort: binding.GatewayPort, WorkspacePath: binding.WorkspacePath}
	proof, ok := s.agentClient.(interface {
		confirmRuntimeFailoverSuccessor(context.Context, models.RuntimeFailoverIntent) error
	})
	if !ok {
		return false, fmt.Errorf("runtime failover supersession requires native publication proof")
	}
	if err = proof.confirmRuntimeFailoverSuccessor(ctx, successor); err != nil {
		return false, err
	}
	retire, ok := store.(repository.RuntimeFailoverRetirementRepository)
	if !ok {
		return false, fmt.Errorf("runtime failover supersession requires conditional retirement")
	}
	retired, err := retire.RetireRuntimeFailover(ctx, prior, successor)
	if err == nil && !retired {
		err = fmt.Errorf("runtime failover successor changed during conditional retirement")
	}
	return retired, err
}

func (c *slotRuntimeAgentClient) confirmRuntimeFailoverSuccessor(ctx context.Context, want models.RuntimeFailoverIntent) error {
	assignments, err := c.assignments.ReservedAssignments(ctx, want.SourceEndpoint, want.SourcePodUID, want.RuntimeType)
	if err != nil {
		return err
	}
	for _, a := range assignments {
		if a.InstanceID != want.InstanceID || a.UserID != want.UserID || a.Generation != want.Generation || a.GatewayID != want.GatewayID {
			continue
		}
		var request RuntimeAgentCreateGatewayRequest
		if json.Unmarshal(a.Request, &request) != nil || request.InstanceID != want.InstanceID || request.UserID != want.UserID || request.Generation != want.Generation || request.AgentType != want.RuntimeType || request.PodUID != want.SourcePodUID || request.GatewayPort != want.GatewayPort || request.WorkspacePath != want.WorkspacePath {
			return fmt.Errorf("runtime failover successor publication differs from current binding")
		}
		return nil
	}
	return fmt.Errorf("runtime failover successor has no matching native publication")
}
