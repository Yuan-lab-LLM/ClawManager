package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

// One-time adoption of pre-attempt-schema records. Migration receipts and
// source generation are still validated by the upgrade reservation transaction.
// Existing bindings are retained, never deleted to make a legacy row retryable.
func (s *RuntimeUpgradeService) initializeUpgradeRecovery(ctx context.Context, item *models.RuntimeUpgradeItem) error {
	if item.RecoveryStartedAt != nil && item.RestartAttempts > 0 {
		return nil
	}
	now := time.Now().UTC()
	_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_started_at = ?, recovery_status = COALESCE(recovery_status, 'pending') WHERE id = ? AND state IN ('migrated','restart_ready') AND recovery_started_at IS NULL`, now, item.ID)
	if err != nil {
		return err
	}
	if err := s.sess.Collection("runtime_upgrade_items").Find("id", item.ID).One(item); err != nil {
		return err
	}
	// An old creating binding may predate this schema. Give its first observed
	// attempt an explicit deadline once, without resetting generation or binding.
	_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET restart_attempts = 1, restart_started_at = ?, restart_deadline_at = ? WHERE id = ? AND restart_attempts = 0 AND EXISTS (SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id = runtime_upgrade_items.instance_id)`, now, now.Add(repository.UpgradeRestartTimeout), item.ID)
	if err != nil {
		return err
	}
	if err := s.sess.Collection("runtime_upgrade_items").Find("id", item.ID).One(item); err != nil {
		return err
	}
	return nil
}

// A read-only Agent observation always precedes timeout handling. This recovers
// successful-but-unacknowledged starts without sending a second create request.
func (s *RuntimeUpgradeService) observeUpgradeGateway(ctx context.Context, item models.RuntimeUpgradeItem, targets []models.RuntimePod) error {
	binding, err := s.bindings.GetByInstanceID(ctx, item.InstanceID)
	if err != nil || binding == nil {
		return err
	}
	agent, ok := s.agent.(interface {
		GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error)
	})
	if !ok {
		return fmt.Errorf("upgrade Gateway observer unavailable")
	}
	if item.SourceGeneration == nil || binding.Generation != *item.SourceGeneration+1 {
		return repository.ErrUpgradeReservationDenied
	}
	var endpoint string
	for _, pod := range targets {
		if pod.ID == binding.RuntimePodID {
			identity := pod.KubernetesPodUID
			if identity == nil {
				identity = pod.PodUID
			}
			if item.RestartTargetPodUID != nil && (identity == nil || *identity != *item.RestartTargetPodUID) {
				return repository.ErrUpgradeReservationDenied
			}
			endpoint = stringValue(pod.AgentEndpoint)
			break
		}
	}
	if endpoint == "" {
		return repository.ErrUpgradeReservationDenied
	}
	id := binding.GatewayID
	if strings.HasPrefix(id, "pending-") {
		id = fmt.Sprintf("gw-%d-%d", item.InstanceID, binding.Generation)
	}
	state, err := agent.GatewayState(ctx, endpoint, id)
	if errors.Is(err, ErrRuntimeAgentNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if state == nil || state.InstanceID != item.InstanceID || state.Generation != binding.Generation || state.GatewayID != id {
		return repository.ErrUpgradeReservationDenied
	}
	if !NormalizeRuntimeGatewayLifecycle(state.State, nil).Running {
		return nil
	}
	now := time.Now().UTC()
	if err := s.bindings.UpdateGatewayAssignment(ctx, item.InstanceID, binding.Generation, id, binding.GatewayPID, "running", &now); err != nil {
		return err
	}
	_, err = s.sess.SQL().ExecContext(ctx, `UPDATE instances SET status = 'running', runtime_error_message = NULL, runtime_error_code = NULL, updated_at = ? WHERE id = ? AND runtime_generation = ? AND status IN ('creating','running')`, now, item.InstanceID, binding.Generation)
	return err
}

func upgradeRecoveryError(err error) (string, bool) {
	if runtimeFailureCode(err) == "PROXY_POLICY_NOT_READY" {
		return "PROXY_POLICY_NOT_READY", false
	}
	switch {
	case errors.Is(err, repository.ErrUpgradeReservationDenied):
		return "UPGRADE_RESERVATION_DENIED", true
	case errors.Is(err, repository.ErrUpgradeRecoveryHeld):
		return "UPGRADE_RECOVERY_REQUIRES_ATTENTION", true
	case errors.Is(err, repository.ErrUpgradeCapacityPending):
		return "UPGRADE_CAPACITY_PENDING", false
	case errors.Is(err, ErrModelConfigurationRequired):
		return "MODEL_CONFIGURATION_REQUIRED", true
	case runtimeUpgradeInfrastructureRetryable(err):
		return "UPGRADE_CONTROL_PLANE_RETRY", false
	default:
		return "UPGRADE_GATEWAY_RETRY", false
	}
}

// A live, heartbeating standby target can still be publishing activation.
// Before any Gateway starts, wait within the existing recovery deadline without
// manufacturing a failed startup or delaying it with a failure retry timestamp.
func (s *RuntimeUpgradeService) waitForUpgradeTargetActivation(ctx context.Context, rollout *models.RuntimeRollout, items []models.RuntimeUpgradeItem) error {
	for _, item := range items {
		if item.RestartAttempts > 0 || item.State == "gateway_verified" || item.State == "verified" {
			return s.recordUnavailableUpgradeTargets(ctx, rollout)
		}
	}
	for _, item := range items {
		if item.State != "migrated" && item.State != "restart_ready" {
			continue
		}
		if err := s.initializeUpgradeRecovery(ctx, &item); err != nil {
			return err
		}
		if item.RecoveryStartedAt != nil && time.Since(*item.RecoveryStartedAt) >= 15*time.Minute {
			return s.recordUnavailableUpgradeTargets(ctx, rollout)
		}
	}
	return nil
}

// Clear only a retryable target-readiness failure after authoritative readiness.
// Held failures, other startup errors, attempts, deadlines and bindings survive.
func (s *RuntimeUpgradeService) clearUnavailableUpgradeTargets(ctx context.Context, rolloutID int64) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_status = CASE WHEN restart_attempts > 0 THEN 'starting' ELSE 'pending' END, recovery_error_code = NULL, error_message = NULL, restart_next_retry_at = NULL WHERE rollout_id = ? AND state IN ('migrated','restart_ready') AND recovery_status = 'retrying' AND recovery_error_code = 'UPGRADE_TARGET_UNAVAILABLE' AND EXISTS (SELECT 1 FROM runtime_rollouts WHERE id = ? AND status = 'running' AND phase = 'gateway_restart')`, rolloutID, rolloutID)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET error_message = NULL WHERE id = ? AND status = 'running' AND phase = 'gateway_restart' AND error_message LIKE 'UPGRADE_TARGET_UNAVAILABLE:%' AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items WHERE rollout_id = ? AND recovery_error_code = 'UPGRADE_TARGET_UNAVAILABLE')`, rolloutID, rolloutID)
		return err
	}, nil)
}

func (s *RuntimeUpgradeService) recordUnavailableUpgradeTargets(ctx context.Context, rollout *models.RuntimeRollout) error {
	items, err := s.listUpgradeItems(ctx, rollout.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.State != "restart_ready" && item.State != "migrated" {
			continue
		}
		if err := s.initializeUpgradeRecovery(ctx, &item); err != nil {
			return err
		}
		if err := s.recordUpgradeRecoveryFailure(ctx, rollout.ID, item, "UPGRADE_TARGET_UNAVAILABLE", "Target Runtime is not ready; existing bindings and upgrade isolation retained", false); err != nil {
			return err
		}
	}
	return nil
}

func (s *RuntimeUpgradeService) recordUpgradeRecoveryFailure(ctx context.Context, rolloutID int64, item models.RuntimeUpgradeItem, code, message string, hold bool) error {
	if item.RecoveryStatus != nil && *item.RecoveryStatus == "needs_attention" {
		return nil
	}
	// Failed observations cannot bypass the bounded deadline forever.
	if upgradeGatewayRestartExpired(item, time.Time{}, time.Now().UTC()) || (item.RecoveryStartedAt != nil && time.Since(*item.RecoveryStartedAt) >= 15*time.Minute) {
		hold = true
	}
	status := "retrying"
	if hold {
		status = "needs_attention"
	}
	now := time.Now().UTC()
	var next *time.Time
	if !hold {
		at := now.Add(15 * time.Second)
		next = &at
	}
	_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_status = ?, recovery_error_code = ?, error_message = ?, restart_next_retry_at = ? WHERE rollout_id = ? AND instance_id = ? AND state IN ('migrated','restart_ready')`, status, code, message, next, rolloutID, item.InstanceID)
	if err != nil {
		return err
	}
	// Keep running: existing schedulers use it as the isolation fence. Error
	// visibility is independent of terminal status, and retries are bounded.
	_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET error_message = ? WHERE id = ? AND status = 'running'`, code+": "+message, rolloutID)
	if err != nil {
		return err
	}
	if item.RecoveryErrorCode == nil || *item.RecoveryErrorCode != code || hold {
		return s.audit(ctx, &rolloutID, nil, "gateway_recovery", "gateway_restart", status, map[string]any{"instance_id": item.InstanceID, "code": code, "attempt": item.RestartAttempts})
	}
	return nil
}
