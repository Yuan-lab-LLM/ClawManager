package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

var errOpenCodeTargetExecutionFailed = errors.New("OpenCode target execution failed; protected native rollback required")

func validateOpenCodeRestore(id int, result *RuntimeAgentSessionSQLiteRestore) error {
	if result == nil || result.InstanceID != id || result.Status != "restored" || !result.ConfigRestored || !result.StateRestored || !result.PreservedSessionSQLite || !validOpenCodeSHA(result.OutputSHA256) {
		return fmt.Errorf("OpenCode rollback lacks verified preservation of current session/config/state")
	}
	return nil
}
func (s *RuntimeScheduler) beginOpenCodeRollback(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, cause error) error {
	for _, item := range p.Instances {
		if validateOpenCodeCapsule(item.ID, item.Capsule) != nil {
			return fmt.Errorf("OpenCode rollback remains held: complete original capsule proof is missing")
		}
	}
	p.Stage = "rollback_stop"
	p.Failure = cause.Error()
	if err := s.saveOpenCodePlan(ctx, r, p); err != nil {
		return err
	}
	return s.setOpenCodePhase(ctx, r, p, "opencode_rollback_stop", "starting")
}
func (s *RuntimeScheduler) advanceOpenCodeRollback(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, backend k8s.OpenCodeUpgradeDeploymentService, client openCodeUpgradeSourceClient) error {
	id := strconv.FormatInt(r.ID, 10)
	upgrade := s.agentClient.(RuntimeUpgradeAgentClient)
	switch p.Stage {
	case "rollback_stop":
		if err := s.setOpenCodePhase(ctx, r, p, "opencode_rollback_stop", "starting"); err != nil {
			return err
		}
		for i := range p.Instances {
			item := &p.Instances[i]
			if item.RollbackState != "" {
				continue
			}
			assignment, state, err := client.openCodeSourceAssignment(ctx, item.ID)
			if err != nil {
				return err
			}
			assignment.Environment = nil
			assignment.Request = nil
			if assignment.UserID != item.Owner || assignment.InstanceID != item.ID || assignment.RuntimeType != RuntimeTypeOpenCode {
				return models.ErrRuntimeUpgradeConflict
			}
			if assignment.Generation == item.Generation {
				if state != "stopped" || assignment.PodUID != item.Assignment.PodUID || assignment.Endpoint != item.Assignment.Endpoint || assignment.GatewayID != item.Assignment.GatewayID {
					return models.ErrRuntimeUpgradeConflict
				}
				var proof []byte
				if item.HistoricalStop {
					proof, err = client.openCodeHistoricalSourceStopProof(ctx, assignment)
				} else {
					proof, err = client.openCodeSourceStopProof(ctx, assignment)
				}
				if err != nil {
					return err
				}
				if string(proof) != string(item.StopProof) {
					return models.ErrRuntimeUpgradeConflict
				}
			} else if assignment.Generation == item.Generation+1 {
				var target *models.RuntimePod
				for j := range p.Targets {
					if stringValue(p.Targets[j].PodUID) == assignment.PodUID && stringValue(p.Targets[j].AgentEndpoint) == assignment.Endpoint {
						target = &p.Targets[j]
					}
				}
				if target == nil || assignment.GatewayID != fmt.Sprintf("gw-%d-%d", item.ID, assignment.Generation) {
					return models.ErrRuntimeUpgradeConflict
				}
				if err = repository.NewWorkspaceWriteFenceRepository(s.upgrade.sess).Fence(ctx, item.ID, item.Owner, assignment.Generation, assignment.PodUID); err != nil {
					return err
				}
				if err = s.agentClient.DeleteGateway(ctx, assignment.Endpoint, assignment.GatewayID); err != nil {
					return err
				}
				proof, err := client.openCodeSourceStopProof(ctx, assignment)
				if err != nil {
					return err
				}
				if err = repository.ReleaseOpenCodeUpgradeRollbackTarget(ctx, s.upgrade.sess, r.ID, p.Operation, item.ID, item.Owner, assignment.Generation, *target, assignment.GatewayID, proof); err != nil {
					return err
				}
				item.TargetStopProof = proof
			} else {
				return models.ErrRuntimeUpgradeConflict
			}
			var generation int
			row, err := s.upgrade.sess.SQL().QueryRowContext(ctx, `SELECT runtime_generation FROM instances WHERE id=? AND user_id=? AND type='opencode'`, item.ID, item.Owner)
			if err != nil {
				return err
			}
			if err = row.Scan(&generation); err != nil {
				return err
			}
			if generation < item.Generation || generation > item.Generation+1 {
				return models.ErrRuntimeUpgradeConflict
			}
			item.RollbackFromGeneration = assignment.Generation
			item.RollbackGeneration = generation + 1
			if item.Status == "stopped" {
				item.RollbackGeneration = item.Generation
			}
			item.RollbackState = "stopped"
			return s.saveOpenCodePlan(ctx, r, p)
		}
		p.Stage = "rollback_restore"
	case "rollback_restore":
		for i := range p.Instances {
			item := &p.Instances[i]
			if item.RollbackState != "stopped" {
				continue
			}
			if validateOpenCodeCapsule(item.ID, item.Capsule) != nil {
				return fmt.Errorf("OpenCode original capsule proof is missing")
			}
			request := RuntimeAgentWorkspaceRequest{ExpectedPodUID: stringValue(p.Targets[0].PodUID), RolloutID: id, SnapshotID: "opencode-" + id, UserID: item.Owner, InstanceID: item.ID, Generation: item.RollbackFromGeneration, ResumeGeneration: item.Generation, PreserveSessionSQLite: true, OfficialDBCheck: true, LeaseToken: runtimeUpgradeLeaseToken(r.ID, item.ID) + "-restore"}
			lease := RuntimeAgentWriterLeaseRequest{RuntimeAgentWorkspaceRequest: request, Token: request.LeaseToken, TTLSeconds: 3600}
			endpoint := stringValue(p.Targets[0].AgentEndpoint)
			if err := upgrade.AcquireWriterLease(ctx, endpoint, lease); err != nil {
				return err
			}
			result, err := upgrade.RestoreSessionSQLite(ctx, endpoint, request)
			if err == nil {
				err = validateOpenCodeRestoreReceipt(request, result)
			}
			if err == nil {
				restoreResult := result
				result, err = upgrade.SessionSQLiteRestoreStatus(ctx, endpoint, request)
				if err == nil && (restoreResult == nil || result == nil || restoreResult.OutputSHA256 != result.OutputSHA256) {
					err = fmt.Errorf("OpenCode restore/status receipts disagree")
				}
				if err == nil {
					err = validateOpenCodeRestoreReceipt(request, result)
				}
			}
			if err != nil {
				return err
			}
			if err = upgrade.ReleaseWriterLease(ctx, endpoint, lease); err != nil {
				return err
			}
			item.Restore = result
			item.RollbackState = "restored"
			return s.saveOpenCodePlan(ctx, r, p)
		}
		p.Stage = "rollback_restart"
	case "rollback_restart":
		if err := s.setOpenCodePhase(ctx, r, p, "opencode_rollback_restart", "waiting"); err != nil {
			return err
		}
		r.Phase = "opencode_rollback_restart"
		reports, err := s.podRepo.List(ctx, RuntimeTypeOpenCode)
		if err != nil {
			return err
		}
		inventory, err := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
		if err != nil {
			return err
		}
		live := upgradePodsWithLiveIdentity(reports, inventory)
		for i := range p.Instances {
			item := &p.Instances[i]
			if item.RollbackState == "verified" {
				continue
			}
			if item.RollbackState == "restored" {
				if err = s.prepareOpenCodeRollbackRestart(ctx, r, p, item); err != nil {
					return err
				}
				item.RollbackState = "restart_ready"
				if err = s.saveOpenCodePlan(ctx, r, p); err != nil {
					return err
				}
			}
			if item.Status == "running" {
				var source *models.RuntimePod
				for j := range live {
					if live[j].ID == item.SourcePod.ID && stringValue(live[j].PodUID) == stringValue(item.SourcePod.PodUID) && live[j].State == "ready" && !live[j].Draining && runtimePodMatchesImage(live[j], item.SourcePod.ImageRef) {
						source = &live[j]
					}
				}
				if source == nil {
					return fmt.Errorf("OpenCode retained source is unavailable; rollback remains fenced")
				}
				sourceRollout := *r
				sourceRollout.TargetImageRef = item.SourcePod.ImageRef
				sourceRollout.TargetImageDigest = item.SourcePod.ImageDigest
				if err = s.EnsureUpgradeGateway(ctx, &sourceRollout, item.ID, []models.RuntimePod{*source}); err != nil {
					return err
				}
				binding, err := s.bindingRepo.GetByInstanceID(ctx, item.ID)
				if err != nil {
					return err
				}
				if binding == nil || binding.Generation != item.RollbackGeneration || binding.RuntimePodID != source.ID || binding.State != "running" {
					return nil
				}
				state, err := upgrade.GatewayState(ctx, stringValue(source.AgentEndpoint), binding.GatewayID)
				if err != nil {
					return err
				}
				if state == nil || state.InstanceID != item.ID || state.Generation != item.RollbackGeneration || !NormalizeRuntimeGatewayLifecycle(state.State, nil).Running {
					return nil
				}
			}
			item.RollbackState = "verified"
			if _, err = s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state='restored',recovery_status='verified',updated_at=UTC_TIMESTAMP(6) WHERE rollout_id=? AND instance_id=?`, r.ID, item.ID); err != nil {
				return err
			}
			return s.saveOpenCodePlan(ctx, r, p)
		}
		p.Stage = "rollback_retire"
	case "rollback_retire":
		for _, source := range p.Sources {
			if err := s.deployments.SetUpgradePoolActive(ctx, source.Target.Namespace, source.Target.Name, runtimeUpgradeTargetDeploymentName(source.Target.Name, r.ID), id, false); err != nil {
				return err
			}
		}
		for _, proof := range p.TargetProofs {
			retired, err := backend.OpenCodeUpgradeSourceRetired(ctx, proof, "rollback-"+id)
			if err == nil {
				if !retired {
					return nil
				}
				continue
			}
			found := false
			for _, target := range p.Targets {
				if target.Namespace != proof.Target.Namespace || target.DeploymentName != proof.Target.Name {
					continue
				}
				found = true
				if err = client.openCodePoolIdle(ctx, target); err != nil {
					return err
				}
				var count int
				row, e := s.upgrade.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=?`, target.ID)
				if e != nil {
					return e
				}
				if e = row.Scan(&count); e != nil {
					return e
				}
				if count != 0 {
					return fmt.Errorf("OpenCode failed target has remaining bindings")
				}
			}
			if !found {
				return fmt.Errorf("OpenCode failed target identity is missing")
			}
			if err = backend.RetireOpenCodeUpgradeSource(ctx, proof, "rollback-"+id); err != nil {
				return err
			}
		}
		p.Stage = "rollback_verify"
	case "rollback_verify":
		for _, proof := range p.TargetProofs {
			retired, err := backend.OpenCodeUpgradeSourceRetired(ctx, proof, "rollback-"+id)
			if err != nil {
				return err
			}
			if !retired {
				return nil
			}
		}
		for _, item := range p.Instances {
			if item.RollbackState != "verified" || validateOpenCodeRestore(item.ID, item.Restore) != nil {
				return fmt.Errorf("OpenCode rollback verification is incomplete")
			}
		}
		return s.commitOpenCodeRollback(ctx, r, p)
	default:
		return fmt.Errorf("unknown OpenCode rollback phase %q", p.Stage)
	}
	return s.saveOpenCodePlan(ctx, r, p)
}
func (s *RuntimeScheduler) prepareOpenCodeRollbackRestart(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, item *openCodeUpgradeInstance) error {
	return s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		var generation int
		var status string
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT runtime_generation,status FROM instances WHERE id=? AND user_id=? AND type='opencode' FOR UPDATE`, item.ID, item.Owner)
		if err != nil {
			return err
		}
		if err = row.Scan(&generation, &status); err != nil {
			return err
		}
		if item.Status == "stopped" {
			if generation != item.Generation || status != "stopped" {
				return models.ErrRuntimeUpgradeConflict
			}
			return nil
		}
		if generation == item.RollbackGeneration && (status == "creating" || status == "running") {
			return nil
		}
		if generation+1 != item.RollbackGeneration || generation < item.Generation || generation > item.Generation+1 || status != "running" && status != "creating" && status != "error" {
			return models.ErrRuntimeUpgradeConflict
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instances SET runtime_generation=?,status='creating',runtime_error_message=NULL,runtime_error_code=NULL,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND runtime_generation=?`, item.RollbackGeneration, item.ID, generation); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state='rollback_ready',restart_target_pod_uid=?,restart_next_retry_at=NULL,updated_at=UTC_TIMESTAMP(6) WHERE rollout_id=? AND instance_id=?`, stringValue(item.SourcePod.PodUID), r.ID, item.ID)
		return err
	}, nil)
}
func (s *RuntimeScheduler) commitOpenCodeRollback(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan) error {
	return s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		for _, item := range p.Instances {
			var generation int
			var status string
			row, err := tx.SQL().QueryRowContext(ctx, `SELECT runtime_generation,status FROM instances WHERE id=? AND user_id=? AND type='opencode' FOR UPDATE`, item.ID, item.Owner)
			if err != nil {
				return err
			}
			if err = row.Scan(&generation, &status); err != nil {
				return err
			}
			if generation != item.RollbackGeneration || status != item.Status {
				return models.ErrRuntimeUpgradeConflict
			}
			result, err := tx.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrades SET state='completed',updated_at=UTC_TIMESTAMP(6) WHERE instance_id=? AND operation_id=? AND state='process_group_confirmed'`, item.ID, p.Operation)
			if err != nil {
				return err
			}
			if n, e := result.RowsAffected(); e != nil {
				return e
			} else if n != 1 {
				return models.ErrRuntimeUpgradeConflict
			}
			if _, err = tx.SQL().ExecContext(ctx, `DELETE FROM instance_lifecycle_manual_guards WHERE instance_id=? AND token=?`, item.ID, "opencode_upgrade_"+p.Operation); err != nil {
				return err
			}
		}
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET phase='opencode_rollback_complete',rollback_status='restored' WHERE id=? AND status='running'`, r.ID); err != nil {
			return err
		}
		now := time.Now().UTC()
		return repository.NewRuntimeRolloutRepository(tx).(interface {
			UpdatePoolReplacementStatus(context.Context, int64, string, string, *time.Time, *time.Time, *string) error
		}).UpdatePoolReplacementStatus(ctx, r.ID, p.raw, "error", r.StartedAt, &now, &p.Failure)
	}, nil)
}
