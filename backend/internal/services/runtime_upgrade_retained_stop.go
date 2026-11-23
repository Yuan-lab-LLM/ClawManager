package services

import (
	"context"
	"fmt"

	"clawreef/internal/models"
)

// A rollback can consume a SQL generation without publishing its successor.
// Its physical source was already stopped. Reissuing Delete under the newer
// SQL generation cannot establish that old stop and must not replace its proof.
// Only explicit recovery under a live migration claim may reuse the existing
// independently finalized source; ordinary and bound stops keep their protocol.
func (s *RuntimeUpgradeService) stopUpgradeMigrationSource(ctx context.Context, rollout *models.RuntimeRollout, c runtimeUpgradeCandidate) (bool, error) {
	scope := runtimeUpgradeScopeFromRollout(rollout)
	if !requiresConfirmedGatewayStop(s.agent) || c.BindingState != "stopped" || (c.InstanceStatus != "error" && c.InstanceStatus != "creating") || !scope.allowsRecovery(c.InstanceID) || scope.RecoveryParentID <= 0 {
		return s.stopCandidateGateway(ctx, c)
	}
	denied := fmt.Errorf("%w: retained source stop is not independently confirmed", ErrUpgradeSourceState)
	claim, _ := ctx.Value(upgradeClaimKey{}).(string)
	reader, ok := s.deployments.(teamStoppedSourceReader)
	if !ok || claim == "" || rollout == nil || c.RuntimePodID == nil || c.SourcePodUID == "" || c.AgentEndpoint == "" || c.GatewayID == "" || c.RuntimeGeneration < 1 || c.Generation != c.RuntimeGeneration {
		return false, denied
	}
	var claimed int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.rollout_id=? AND ui.instance_id=? AND ui.state='quiescing' AND ui.operation_phase='session_migration' AND ui.operation_token=? AND ui.operation_deadline_at>UTC_TIMESTAMP(6) AND r.status='running' AND r.phase='session_migration'`, rollout.ID, c.InstanceID, claim)
	if err != nil {
		return false, err
	}
	if err := row.Scan(&claimed); err != nil {
		return false, err
	}
	if claimed != 1 {
		return false, denied
	}
	var user, generation int
	var kind, status, workspace string
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,type,status,workspace_path FROM instances WHERE id=?`, c.InstanceID)
	if err != nil {
		return false, err
	}
	if err := row.Scan(&user, &generation, &kind, &status, &workspace); err != nil {
		return false, err
	}
	if kind != RuntimeTypeOpenClaw || user != c.UserID || generation != c.RuntimeGeneration || status != c.InstanceStatus || workspace != c.WorkspacePath {
		return false, denied
	}
	if binding, err := s.bindings.GetByInstanceID(ctx, c.InstanceID); err != nil {
		return false, err
	} else if binding != nil {
		return false, denied
	}
	// The reader validates original container exits, writer release, storage
	// identity, and absence of any newer or uncertain publication. Missing SQL
	// bindings or a native 404 alone never supply this authority.
	proof, err := reader.ConfirmedTeamStoppedSource(ctx, c.InstanceID, user, generation, workspace)
	if err != nil {
		return false, err
	}
	if proof.InstanceID != c.InstanceID || proof.UserID != user || proof.Generation < 1 || proof.Generation > generation || proof.Phase != "completed" || proof.StorageIdentity == "" || proof.WorkspacePath != workspace || proof.RuntimePodID != *c.RuntimePodID || proof.PodUID != c.SourcePodUID || proof.GatewayID != c.GatewayID || proof.Endpoint != c.AgentEndpoint {
		return false, denied
	}
	if binding, err := s.bindings.GetByInstanceID(ctx, c.InstanceID); err != nil {
		return false, err
	} else if binding != nil {
		return false, denied
	}
	return true, nil
}
