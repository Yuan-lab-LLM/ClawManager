package services

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func pendingPublishedStart(instance models.Instance, binding *models.InstanceRuntimeBinding) bool {
	return instance.Status == "creating" && instance.AutoStopPhase == "" && instance.RuntimeSecurityProfile == RuntimeSecurityProfile &&
		binding != nil && binding.InstanceID == instance.ID && binding.Generation == instance.RuntimeGeneration &&
		binding.RuntimeType == instance.Type && binding.State == RuntimeGatewayBindingCreating &&
		binding.GatewayID == fmt.Sprintf("pending-%d-%d", instance.ID, instance.RuntimeGeneration)
}

func ownsPendingStartRecovery(instance models.Instance, binding models.InstanceRuntimeBinding, recovery *models.InstanceRuntimeRecovery) bool {
	return pendingPublishedStart(instance, &binding) && recovery != nil && recovery.InstanceID == instance.ID &&
		recovery.Generation == instance.RuntimeGeneration && recovery.Reason == repository.PendingPublishedStartRecoveryReason &&
		recovery.SourcePodID == binding.RuntimePodID && recovery.SourceGatewayPort == binding.GatewayPort && recovery.Phase == ""
}

// This is an explicit administrator retry of an existing failed start, including
// older Agents that only report gateway_start_failed. Evidence authorizes the
// usual confirmed stop, never a release or launch on its own.
func (s *RuntimeScheduler) verifyPendingPublishedFailure(ctx context.Context, instance models.Instance, binding models.InstanceRuntimeBinding, admitted bool) (bool, error) {
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || s.podRepo == nil || !pendingPublishedStart(instance, &binding) || runtimeRecoveryPermanentFailure(instance, &binding) {
		return false, nil
	}
	source, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return false, err
	}
	if source == nil || source.RuntimeType != instance.Type || stringValue(source.PodUID) == "" || stringValue(source.AgentEndpoint) == "" {
		return false, nil
	}
	endpoint, uid := *source.AgentEndpoint, *source.PodUID
	pod, err := client.containerStopPod(ctx, endpoint, uid)
	if err != nil {
		return false, err
	}
	if pod.Name != source.PodName || pod.Namespace != source.Namespace || pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		return false, nil
	}
	gateway := fmt.Sprintf("gw-%d-%d", instance.ID, instance.RuntimeGeneration)
	a, state, intent, _, err := client.assignments.ReadContainerStop(ctx, endpoint, gateway)
	if err != nil {
		return false, err
	}
	var req RuntimeAgentCreateGatewayRequest
	if a.InstanceID != instance.ID || a.UserID != instance.UserID || a.Generation != binding.Generation || a.PodUID != uid || a.RuntimeType != instance.Type ||
		json.Unmarshal(a.Request, &req) != nil || req.InstanceID != a.InstanceID || req.UserID != a.UserID || req.Generation != a.Generation ||
		req.PodUID != uid || req.AgentType != a.RuntimeType || req.SecurityProfile != RuntimeSecurityProfile ||
		req.GatewayPort != binding.GatewayPort || req.WorkspacePath != binding.WorkspacePath ||
		req.WorkspacePath != RuntimeWorkspacePathWithRoot(s.workspaceRoot, instance.Type, instance.UserID, instance.ID) {
		return false, nil
	}
	slot, err := containerSlotName(pod, a.Request)
	if err != nil {
		return false, err
	}
	if intent != nil {
		return admitted && validContainerIntent(*intent) && intent.Slot == slot && !intent.Graceful && intent.Kind == "", nil
	}
	if state != "running" && !(admitted && state == "draining") {
		return false, nil
	}
	observation := runtimeSlotAuditObservation{Name: slot, Port: binding.GatewayPort}
	client.observeRuntimeSlot(ctx, *source, &observation)
	if observation.Err != nil {
		return false, observation.Err
	}
	return pendingPublishedSlotFailed(observation.Snapshot, instance, gateway), nil
}

func pendingPublishedSlotFailed(snapshot runtimeSlotAuditSnapshot, instance models.Instance, gateway string) bool {
	owner := snapshot.Assignment
	digest, err := hex.DecodeString(snapshot.Digest)
	if owner == nil || owner.GatewayID != gateway || owner.InstanceID != instance.ID || owner.UserID != instance.UserID ||
		owner.Generation != instance.RuntimeGeneration || owner.WorkerEpoch != snapshot.WorkerEpoch ||
		owner.State != "starting" || snapshot.IdleConfirmed == nil || *snapshot.IdleConfirmed ||
		err != nil || len(digest) != 32 || snapshot.MaintenanceOperationID != "" || len(snapshot.Gateways) != 1 {
		return false
	}
	g := snapshot.Gateways[0]
	return g.GatewayID == gateway && (g.State == "error" || g.State == "failed") && g.ErrorMessage != "" &&
		(g.ErrorMessage == "gateway_start_failed" || !runtimeRecoveryFreshFailureBlocked("", g.ErrorMessage))
}
