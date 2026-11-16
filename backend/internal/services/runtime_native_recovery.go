package services

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const runtimeRecoveryNativeResetReason = "native gateway automatic recovery requires confirmed container reset"

// This compatibility path admits only the old native worker's lost crash cause.
// Its verdict authorizes the existing confirmed-stop protocol, never placement.
func (s *RuntimeScheduler) nativeRecoveryCandidate(ctx context.Context, instance models.Instance, binding *models.InstanceRuntimeBinding, recovery *models.InstanceRuntimeRecovery) (bool, error) {
	if instance.Type != RuntimeTypeOpenClaw || instance.Status != "error" || instance.AutoStopPhase != "" || runtimeRecoveryPermanentFailure(instance, binding) || binding == nil || binding.State != "error" || stringValue(binding.ErrorMessage) != "gateway_stop_failed" || binding.InstanceID != instance.ID || binding.Generation != instance.RuntimeGeneration || binding.RuntimeType != RuntimeTypeOpenClaw {
		return false, nil
	}
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || s.podRepo == nil {
		return false, nil
	}
	source, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return false, err
	}
	if source == nil || source.RuntimeType != RuntimeTypeOpenClaw || stringValue(source.PodUID) == "" || stringValue(source.AgentEndpoint) == "" {
		return false, nil
	}
	endpoint := *source.AgentEndpoint
	pod, err := client.containerStopPod(ctx, endpoint, *source.PodUID)
	if err != nil {
		return false, err
	}
	if pod.Name != source.PodName || pod.Namespace != source.Namespace || pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		return false, nil
	}
	a, state, intent, _, err := client.assignments.ReadContainerStop(ctx, endpoint, binding.GatewayID)
	if err != nil {
		return false, err
	}
	if a.InstanceID != instance.ID || a.UserID != instance.UserID || a.Generation != instance.RuntimeGeneration || a.GatewayID != binding.GatewayID || a.PodUID != *source.PodUID || a.RuntimeType != instance.Type {
		return false, nil
	}
	var req RuntimeAgentCreateGatewayRequest
	if json.Unmarshal(a.Request, &req) != nil || req.InstanceID != instance.ID || req.UserID != instance.UserID || req.Generation != instance.RuntimeGeneration || req.PodUID != a.PodUID || req.AgentType != instance.Type || req.GatewayPort != binding.GatewayPort || req.WorkspacePath != binding.WorkspacePath || instance.WorkspacePath == nil || req.WorkspacePath != *instance.WorkspacePath {
		return false, nil
	}
	slot, err := containerSlotName(pod, a.Request)
	if err != nil {
		return false, err
	}
	// A previously admitted recovery may have already restarted the container.
	// Continue only its durable, exact non-graceful stop; the stop protocol must
	// still establish termination, cleanup and writer-release receipts.
	ownsRecovery := recovery != nil && recovery.InstanceID == instance.ID && recovery.Reason == runtimeRecoveryNativeResetReason && recovery.Generation == instance.RuntimeGeneration && recovery.Attempts > 0 && recovery.SourcePodID == binding.RuntimePodID && recovery.SourceGatewayPort == binding.GatewayPort
	if intent != nil {
		return ownsRecovery && validContainerIntent(*intent) && intent.Slot == slot && !intent.Graceful && intent.Kind == "", nil
	}
	// MarkDraining precedes stop-intent persistence. An interrupted admitted
	// attempt may retry that gap, but must still verify fresh provenance below.
	if state != "running" && (state != "draining" || !ownsRecovery) {
		return false, nil
	}
	var snapshot struct {
		PodUID                 string `json:"pod_uid"`
		WorkerEpoch            string `json:"worker_epoch"`
		Digest                 string `json:"digest"`
		Quarantined            bool   `json:"quarantined"`
		IdleConfirmed          *bool  `json:"idle_confirmed"`
		MaintenanceOperationID string `json:"maintenance_operation_id"`
		Assignment             struct {
			PodUID      string `json:"pod_uid"`
			WorkerEpoch string `json:"worker_epoch"`
			GatewayID   string `json:"gateway_id"`
			InstanceID  int    `json:"instance_id"`
			UserID      int    `json:"user_id"`
			Generation  int    `json:"generation"`
			State       string `json:"state"`
			IdleStop    bool   `json:"idle_stop"`
		} `json:"assignment"`
		Gateways []struct {
			GatewayID      string `json:"gateway_id"`
			InstanceID     int    `json:"instance_id"`
			UserID         int    `json:"user_id"`
			Generation     int    `json:"generation"`
			RuntimeType    string `json:"runtime_type"`
			Port           int    `json:"gateway_port"`
			State          string `json:"state"`
			ErrorMessage   string `json:"error_message"`
			RestartAttempt int    `json:"restart_attempt"`
		} `json:"gateways"`
	}
	if err := client.do(ctx, http.MethodGet, endpoint, "/v1/slots/"+slot+"/state", nil, &snapshot); err != nil {
		return false, err
	}
	owner := snapshot.Assignment
	digest, digestErr := hex.DecodeString(snapshot.Digest)
	if snapshot.PodUID != a.PodUID || !snapshot.Quarantined || (snapshot.IdleConfirmed == nil || *snapshot.IdleConfirmed) || snapshot.MaintenanceOperationID != "" || owner.PodUID != a.PodUID || owner.WorkerEpoch != snapshot.WorkerEpoch || owner.GatewayID != a.GatewayID || owner.InstanceID != a.InstanceID || owner.UserID != a.UserID || owner.Generation != a.Generation || owner.IdleStop || (owner.State != "starting" && owner.State != "running") || (!stopTokenValid(snapshot.WorkerEpoch) || digestErr != nil || len(digest) != 32) || len(snapshot.Gateways) != 1 {
		return false, nil
	}
	gateway := snapshot.Gateways[0]
	if gateway.GatewayID != a.GatewayID || gateway.InstanceID != a.InstanceID || gateway.UserID != a.UserID || gateway.Generation != a.Generation || gateway.RuntimeType != instance.Type || gateway.Port != binding.GatewayPort || gateway.State != "error" || gateway.ErrorMessage != "gateway_stop_failed" || gateway.RestartAttempt <= 0 {
		return false, nil
	}
	return true, nil
}

func (s *RuntimeScheduler) confirmNativeRecoveryCandidate(ctx context.Context, instance models.Instance, binding *models.InstanceRuntimeBinding) error {
	store, ok := s.instanceRepo.(repository.InstanceRuntimeRecoveryRepository)
	if !ok {
		return fmt.Errorf("native recovery requires durable intent")
	}
	recovery, err := store.GetRuntimeRecovery(ctx, instance.ID)
	if err != nil {
		return err
	}
	verified, err := s.nativeRecoveryCandidate(ctx, instance, binding, recovery)
	if err != nil {
		return err
	}
	if !verified {
		return fmt.Errorf("native automatic recovery source is no longer verified")
	}
	return nil
}
