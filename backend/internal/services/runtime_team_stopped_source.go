package services

import (
	"clawreef/internal/models"
	k8sservice "clawreef/internal/services/k8s"
	"context"
	"fmt"
)

type teamStoppedSourceReader interface {
	ConfirmedTeamStoppedSource(context.Context, int, int, int, string) (models.InstanceIdleStopOperation, error)
}

// Reuse actual finalized container evidence when error cleanup did not pass
// through the idle-stop SQL ledger. Identity/storage/version checks remain in
// the existing cold-placement path; this does not create a synthetic SQL stop.
func (s *RuntimeScheduler) ConfirmedTeamStoppedSource(ctx context.Context, id, user, generation int, workspace string) (models.InstanceIdleStopOperation, error) {
	c, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || c.assignments == nil || c.kube == nil {
		return models.InstanceIdleStopOperation{}, fmt.Errorf("confirmed retained source unavailable")
	}
	a, err := c.assignments.RetainedStoppedAssignment(ctx, id, user, generation, RuntimeTypeOpenClaw, workspace)
	if err != nil {
		return models.InstanceIdleStopOperation{}, err
	}
	_, state, intent, raw, err := c.assignments.ReadContainerStop(ctx, a.Endpoint, a.GatewayID)
	if err != nil {
		return models.InstanceIdleStopOperation{}, err
	}
	var proof runtimeSlotContainerProof
	if state != "stopped" || intent == nil || !validContainerIntent(*intent) || decodeContainerProof(raw, &proof) != nil || proof.runtimeSlotStopIntent != *intent || !validSlotProofSource(proof) || !proof.CleanupConfirmed || proof.ContainerID == "" || proof.FinishedAt.IsZero() || proof.InstanceID != id || proof.UserID != user || proof.Generation != a.Generation || proof.PodUID != a.PodUID || proof.GatewayID != a.GatewayID {
		return models.InstanceIdleStopOperation{}, fmt.Errorf("retained source termination proof changed")
	}
	pods, err := s.podRepo.List(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return models.InstanceIdleStopOperation{}, err
	}
	for _, p := range pods {
		if stringValue(p.PodUID) != a.PodUID || stringValue(p.AgentEndpoint) != a.Endpoint {
			continue
		}
		storage, err := k8sservice.ResolveRuntimePersistentStorage(ctx, c.kube, p.Namespace, p.PodName, a.PodUID, workspace)
		if err != nil {
			return models.InstanceIdleStopOperation{}, err
		}
		return models.InstanceIdleStopOperation{InstanceID: id, UserID: user, Generation: a.Generation, RuntimePodID: p.ID, GatewayID: a.GatewayID, PodUID: a.PodUID, Endpoint: a.Endpoint, WorkspacePath: workspace, StorageIdentity: storage, Phase: "completed", CreatedAt: proof.FinishedAt}, nil
	}
	return models.InstanceIdleStopOperation{}, fmt.Errorf("retained source Pod identity unavailable")
}
