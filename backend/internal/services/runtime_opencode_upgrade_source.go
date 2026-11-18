package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
)

type openCodeUpgradeSourceClient interface {
	openCodeSourceAssignment(context.Context, int) (k8s.RuntimeAssignment, string, error)
	openCodeSourceStopProof(context.Context, k8s.RuntimeAssignment) (json.RawMessage, error)
	openCodeHistoricalSourceStopProof(context.Context, k8s.RuntimeAssignment) (json.RawMessage, error)
	openCodePoolIdle(context.Context, models.RuntimePod) error
	openCodeActivateUpgrade(context.Context, models.RuntimePod, string) error
}

func (c *slotRuntimeAgentClient) openCodeSourceAssignment(ctx context.Context, id int) (k8s.RuntimeAssignment, string, error) {
	return c.assignments.OpenCodeUpgradeSource(ctx, id)
}
func (c *slotRuntimeAgentClient) openCodeHistoricalSourceStopProof(ctx context.Context, want k8s.RuntimeAssignment) (json.RawMessage, error) {
	return c.openCodeStoppedWriterProof(ctx, want, true)
}
func (c *slotRuntimeAgentClient) openCodeSourceStopProof(ctx context.Context, want k8s.RuntimeAssignment) (json.RawMessage, error) {
	return c.openCodeStoppedWriterProof(ctx, want, false)
}
func (c *slotRuntimeAgentClient) openCodeStoppedWriterProof(ctx context.Context, want k8s.RuntimeAssignment, historical bool) (json.RawMessage, error) {
	actual, state, intent, raw, err := c.assignments.ReadContainerStop(ctx, want.Endpoint, want.GatewayID)
	if err != nil {
		return nil, err
	}
	if state != "stopped" || actual.InstanceID != want.InstanceID || actual.UserID != want.UserID || actual.Generation != want.Generation || actual.PodUID != want.PodUID || actual.RuntimeType != "opencode" || intent == nil || !validContainerIntent(*intent) {
		return nil, fmt.Errorf("OpenCode source has no exact confirmed C1 stop")
	}
	var proof runtimeSlotContainerProof
	if err = decodeContainerProof(raw, &proof); err != nil {
		return nil, err
	}
	if proof.runtimeSlotStopIntent != *intent || proof.ContainerID == "" || proof.FinishedAt.IsZero() || !proof.CleanupConfirmed && !proof.AuxiliaryExitPending {
		return nil, fmt.Errorf("OpenCode source exact container/writer termination proof is incomplete")
	}
	if err = c.assignments.OpenCodeUpgradeStopReleased(ctx, want.InstanceID, *intent); err != nil {
		return nil, err
	}
	identity := runtimeauth.Identity{InstanceID: want.InstanceID, UserID: want.UserID, Generation: want.Generation, PodUID: want.PodUID}
	if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
		return nil, err
	}
	if historical {
		err = c.requireExternalWriterAbsence(ctx, identity)
	} else {
		err = c.requireExternalWriterCleanup(ctx, identity)
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *slotRuntimeAgentClient) openCodePoolIdle(ctx context.Context, pod models.RuntimePod) error {
	slots, err := c.observeRuntimeSlots(ctx, pod)
	if err != nil {
		return err
	}
	if len(slots) != pod.Capacity || len(slots) == 0 {
		return fmt.Errorf("OpenCode physical slot inventory is incomplete")
	}
	for _, slot := range slots {
		if !slot.free() {
			return fmt.Errorf("OpenCode source slot %s is occupied or has unknown writer state", slot.Name)
		}
	}
	return nil
}

// An already stopped instance may retain a historical C1 assignment after its
// source Pod was retired. Only a completed native preservation plan can bridge
// that immutable writer receipt to the current controller and same storage.
func (s *RuntimeScheduler) completedOpenCodeStoppedSource(ctx context.Context, item openCodeUpgradeInstance, assignment k8s.RuntimeAssignment, current *k8s.OpenCodeUpgradeSource) (models.RuntimePod, int64, error) {
	var raw string
	var rolloutID int64
	row, err := s.upgrade.sess.SQL().QueryRowContext(ctx, `SELECT r.id,c.state_json FROM runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id JOIN runtime_upgrade_items i ON i.rollout_id=r.id AND i.instance_id=? WHERE r.runtime_type='opencode' AND r.status='finished' ORDER BY r.id DESC LIMIT 1`, item.ID)
	if err != nil {
		return models.RuntimePod{}, 0, err
	}
	if err = row.Scan(&rolloutID, &raw); err != nil {
		return models.RuntimePod{}, 0, fmt.Errorf("stopped OpenCode instance %d has no completed native source provenance: %w", item.ID, err)
	}
	var previous openCodeUpgradePlan
	if json.Unmarshal([]byte(raw), &previous) != nil || previous.Kind != "opencode-native-preserve" || previous.Strategy != RuntimeUpgradeStrategyOpenCodePreserve || previous.Stage != "verify" {
		return models.RuntimePod{}, 0, fmt.Errorf("OpenCode historical source completion proof is invalid")
	}
	matchesController := false
	for _, proof := range previous.TargetProofs {
		if proof.Target.Namespace == current.Target.Namespace && proof.Target.Name == current.Target.Name && proof.Target.UID == current.Target.UID && proof.StorageIdentity == current.StorageIdentity && proof.Target.Image == current.Target.Image {
			matchesController = true
		}
	}
	if !matchesController {
		return models.RuntimePod{}, 0, fmt.Errorf("OpenCode historical stopped source no longer matches current controller/storage lineage")
	}
	for _, prior := range previous.Instances {
		if prior.ID != item.ID {
			continue
		}
		if prior.Owner != item.Owner || prior.Generation != item.Generation || prior.Status != "stopped" || prior.State != "verified" || prior.Workspace != item.Workspace || prior.Assignment.InstanceID != assignment.InstanceID || prior.Assignment.UserID != assignment.UserID || prior.Assignment.Generation != assignment.Generation || prior.Assignment.PodUID != assignment.PodUID || prior.Assignment.Endpoint != assignment.Endpoint || prior.Assignment.GatewayID != assignment.GatewayID || len(prior.StopProof) == 0 || validateOpenCodeCapsuleReceipt(RuntimeAgentWorkspaceRequest{RolloutID: strconv.FormatInt(rolloutID, 10), InstanceID: item.ID, Generation: item.Generation}, prior.Capsule, prior.Compatibility) != nil {
			return models.RuntimePod{}, 0, models.ErrRuntimeUpgradeConflict
		}
		return prior.SourcePod, rolloutID, nil
	}
	return models.RuntimePod{}, 0, models.ErrRuntimeUpgradeConflict
}

func (c *slotRuntimeAgentClient) openCodeActivateUpgrade(ctx context.Context, pod models.RuntimePod, rolloutID string) error {
	uid := stringValue(pod.PodUID)
	endpoint := stringValue(pod.AgentEndpoint)
	if uid == "" || endpoint == "" {
		return fmt.Errorf("OpenCode activation requires exact frozen Pod identity")
	}
	if _, err := c.containerStopPod(ctx, endpoint, uid); err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, endpoint, "/v1/openclaw/upgrade/activate", map[string]string{"rollout_id": rolloutID, "expected_pod_uid": uid}, nil)
}
