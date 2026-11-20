package services

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
)

// Only a durably completed pre-activation rollback may reuse its restored data.
// Missing SQL bindings, elapsed deadlines and absent Secrets are not stop proof:
// the existing reader independently validates the finalized container proof,
// current storage and the absence of ANY newer/unknown published generation.
func (s *RuntimeUpgradeService) verifyUnpublishedRestoredSource(ctx context.Context, r *models.RuntimeRollout, item models.RuntimeUpgradeItem, generation int) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	denied := fmt.Errorf("%w: restored source has no finalized unpublished continuation", ErrUpgradeSourceState)
	if r == nil || r.Status != "error" || !r.AutoRollback || r.Phase != "session_migration" || item.IsolatedAt == nil || stringValue(item.IsolationPhase) != "session_migration" || item.State != "restart_ready" || item.RecoveryStartedAt == nil || stringValue(item.OperationPhase) != "rollback_restore" || stringValue(item.RecoveryStatus) != "needs_attention" || item.RestartAttempts != 0 || item.RestartTargetPodUID != nil || item.SourceGeneration == nil || *item.SourceGeneration+1 != generation || item.RuntimePodID == nil || stringValue(item.SourcePodUID) == "" || stringValue(item.SourceImageDigest) == "" || stringValue(item.SourceRuntimeVersion) == "" {
		return denied
	}
	binding, err := s.bindings.GetByInstanceID(ctx, item.InstanceID)
	if err != nil {
		return err
	}
	if binding != nil {
		return denied
	}
	reader, ok := s.deployments.(teamStoppedSourceReader)
	if !ok {
		return denied
	}
	var user int
	var workspace string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT user_id,workspace_path FROM instances WHERE id=? AND runtime_generation=?`, item.InstanceID, generation)
	if err != nil {
		return err
	}
	if err = row.Scan(&user, &workspace); err != nil {
		return err
	}
	proof, err := reader.ConfirmedTeamStoppedSource(ctx, item.InstanceID, user, generation, workspace)
	if err != nil {
		return err
	}
	pod, err := s.pods.GetByID(ctx, *item.RuntimePodID)
	if err != nil {
		return err
	}
	if pod == nil || !persistedSourcePodMatches(*pod, item) || proof.Phase != "completed" || proof.InstanceID != item.InstanceID || proof.UserID != user || proof.Generation != *item.SourceGeneration || proof.RuntimePodID != *item.RuntimePodID || proof.GatewayID != stringValue(item.SourceGatewayID) || proof.PodUID != stringValue(item.SourcePodUID) || proof.Endpoint != stringValue(pod.AgentEndpoint) || proof.WorkspacePath != workspace || proof.StorageIdentity == "" {
		return denied
	}
	return s.verifyRecoverySourceStopped(ctx, item)
}
