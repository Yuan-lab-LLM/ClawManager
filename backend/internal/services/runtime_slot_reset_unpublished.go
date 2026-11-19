package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"clawreef/internal/models"
	k8sservice "clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func validUnpublishedSlotReset(j *RuntimeSlotResetJournal, userID int) bool {
	b := j.SourceBinding
	return j.Owner == nil && j.UnpublishedReservation != nil && j.UnpublishedReservation.UserID == userID && userID > 0 && stopTokenValid(j.UnpublishedReservation.WorkerEpoch) && b != nil && b.ID > 0 && b.InstanceID == j.InstanceID && b.RuntimePodID == j.Source.ID && b.RuntimeType == j.Source.RuntimeType && b.GatewayPort == j.GatewayPort && b.Generation == 1 && b.GatewayID == fmt.Sprintf("pending-%d-1", j.InstanceID)
}

func (s *RuntimeManagementService) confirmUnpublishedSlotReset(ctx context.Context, j *RuntimeSlotResetJournal, instance models.Instance) error {
	b := j.SourceBinding
	if b == nil || b.Generation != 1 || b.GatewayID != fmt.Sprintf("pending-%d-1", instance.ID) || instance.RuntimeGeneration != 1 || !isSchedulerManagedV2Instance(instance) || instance.Type != j.Source.RuntimeType || j.Source.AgentEndpoint == nil || b.WorkspacePath != RuntimeWorkspacePathWithRoot(s.scheduler.workspaceRoot, instance.Type, instance.UserID, instance.ID) {
		return errors.New("old source owner is unavailable; current binding was retained")
	}
	c := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	pod, err := c.kube.CoreV1().Pods(j.Scope.Namespace).Get(ctx, j.Scope.PodName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(pod.UID) != j.PodUID || pod.Spec.NodeName != j.Scope.NodeName {
		return errors.New("unpublished source physical identity changed")
	}
	replacement := false
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name == j.Slot && st.State.Running != nil && st.ContainerID != "" && st.ContainerID != j.ContainerID {
			replacement = true
		}
	}
	if !replacement {
		return k8sservice.ErrRuntimeFaultEvictionPending
	}
	var state runtimeSlotAuditSnapshot
	if err = c.do(ctx, http.MethodGet, *j.Source.AgentEndpoint, "/v1/slots/"+j.Slot+"/state", nil, &state); err != nil {
		return fmt.Errorf("%w: inspect unpublished replacement: %v", k8sservice.ErrRuntimeFaultEvictionPending, err)
	}
	if state.PodUID != j.PodUID || !stopTokenValid(state.WorkerEpoch) || state.Assignment != nil || !(runtimeSlotAuditObservation{Snapshot: state}).free() {
		return errors.New("unpublished replacement lacks an authenticated empty book")
	}
	if err = c.assignments.CanRetryUnpublished(ctx, k8sservice.RuntimeAssignment{InstanceID: instance.ID, UserID: instance.UserID, Generation: 1, RuntimeType: instance.Type, Endpoint: *j.Source.AgentEndpoint, PodUID: j.PodUID, GatewayID: fmt.Sprintf("gw-%d-1", instance.ID)}); err != nil {
		return err
	}
	// This also inventories historical/unlabelled reserved Secret names. A 404
	// ConfigMap alone is never proof that the generation had no business writer.
	if err = c.assignments.CheckUnboundInstanceStopped(ctx, instance.ID, instance.UserID, 1, instance.Type, nil); err != nil {
		return err
	}
	j.UnpublishedReservation = &runtimeSlotResetUnpublishedProof{UserID: instance.UserID, WorkerEpoch: state.WorkerEpoch}
	if !validUnpublishedSlotReset(j, instance.UserID) {
		return errors.New("unpublished SQL reservation identity changed")
	}
	return nil
}
