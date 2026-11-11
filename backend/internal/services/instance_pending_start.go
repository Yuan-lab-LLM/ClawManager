package services

import (
	"context"
	"fmt"
	"strings"

	"clawreef/internal/models"
)

// A timed-out unpublished start retains its exact SQL reservation. An explicit
// retry can resume that generation after the normal assignment service proves
// its predecessor stopped and no successor credentials were published. It never
// grants stop authority, releases a slot, or overwrites an immutable request.
func (s *instanceService) resumeUnpublishedV2Start(ctx context.Context, instance *models.Instance, runtimeType string) (bool, error) {
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || s.bindingRepo == nil || s.runtimePodRepo == nil || (instance.Status != "stopped" && instance.Status != "error") {
		return false, nil
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return false, err
	}
	if binding == nil || binding.InstanceID != instance.ID || binding.RuntimeType != runtimeType || binding.Generation != instance.RuntimeGeneration || binding.State != RuntimeGatewayBindingCreating || binding.GatewayID != fmt.Sprintf("pending-%d-%d", instance.ID, instance.RuntimeGeneration) {
		return false, nil
	}
	workspace := RuntimeWorkspacePathWithRoot(s.runtimeWorkspaceRoot(), runtimeType, instance.UserID, instance.ID)
	if binding.WorkspacePath != workspace || stringValue(instance.WorkspacePath) != workspace {
		return false, fmt.Errorf("pending start workspace does not match instance")
	}
	pod, err := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return false, err
	}
	if pod == nil || pod.State != "ready" || pod.Draining || pod.RuntimeType != runtimeType || strings.TrimSpace(stringValue(pod.AgentEndpoint)) == "" || strings.TrimSpace(stringValue(pod.PodUID)) == "" {
		return false, nil
	}
	if err := client.canRetryPendingGateway(ctx, *pod.AgentEndpoint, RuntimeAgentCreateGatewayRequest{InstanceID: instance.ID, UserID: instance.UserID, Generation: instance.RuntimeGeneration, AgentType: runtimeType, PodUID: *pod.PodUID}); err != nil {
		// Published or uncertain starts retain the existing confirmed teardown
		// path; a failed proof never permits discarding the pending binding.
		return false, nil
	}
	resumer, ok := s.instanceRepo.(interface {
		ResumeUnpublishedRuntimeStart(context.Context, models.Instance, models.InstanceRuntimeBinding) (bool, error)
	})
	if !ok {
		return false, fmt.Errorf("pending start retry requires conditional persistence")
	}
	changed, err := resumer.ResumeUnpublishedRuntimeStart(ctx, *instance, *binding)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, models.ErrInstanceStopping
	}
	instance.Status = "creating"
	instance.RuntimeErrorMessage, instance.RuntimeErrorCode = nil, nil
	GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	return true, nil
}
