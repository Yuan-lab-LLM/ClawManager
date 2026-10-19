package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

var (
	ErrUpgradeReservationDenied = errors.New("UPGRADE_RESERVATION_DENIED")
	ErrUpgradeCapacityPending   = errors.New("UPGRADE_CAPACITY_PENDING")
	ErrUpgradeRecoveryHeld      = errors.New("UPGRADE_RECOVERY_REQUIRES_ATTENTION")
)

const UpgradeRestartMaxAttempts = 3
const UpgradeRestartTimeout = 5 * time.Minute

// This is deliberately not a bypass flag on ordinary TryClaimSlot. Only the
// data-safe upgrade controller can reserve a binding through this contract.
type UpgradeGatewayReservation struct {
	RolloutID                         int64
	InstanceID, Generation            int
	Pod                               models.RuntimePod
	WorkspacePath                     string
	PortStart, PortEnd, PortBlockSize int
}

type UpgradeGatewayReserver interface {
	ReserveUpgradeGateway(context.Context, UpgradeGatewayReservation) (*models.InstanceRuntimeBinding, error)
}

// ReserveUpgradeGateway commits capacity, port, binding and attempt together.
// All callers lock in rollout -> item -> instance -> pod order. No network or
// workspace operations are performed while holding these database locks.
func (r *instanceRuntimeBindingRepository) ReserveUpgradeGateway(ctx context.Context, req UpgradeGatewayReservation) (*models.InstanceRuntimeBinding, error) {
	identity := req.Pod.KubernetesPodUID
	if identity == nil {
		identity = req.Pod.PodUID
	}
	if req.RolloutID <= 0 || req.InstanceID <= 0 || req.Generation <= 0 || identity == nil || *identity == "" || req.PortBlockSize <= 0 || req.PortStart <= 0 || req.PortEnd < req.PortStart {
		return nil, ErrUpgradeReservationDenied
	}
	var binding *models.InstanceRuntimeBinding
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		var rollout models.RuntimeRollout
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_rollouts WHERE id = ? FOR UPDATE", req.RolloutID); err != nil {
			return err
		}
		if err := tx.Collection("runtime_rollouts").Find("id", req.RolloutID).One(&rollout); err != nil {
			return err
		}
		if rollout.RuntimeType != "openclaw" || rollout.Status != "running" || rollout.Phase != "gateway_restart" || rollout.PreflightID == nil || rollout.TargetImageDigest == nil {
			return ErrUpgradeReservationDenied
		}
		var item models.RuntimeUpgradeItem
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_upgrade_items WHERE rollout_id = ? AND instance_id = ? FOR UPDATE", req.RolloutID, req.InstanceID); err != nil {
			return err
		}
		if err := tx.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": req.RolloutID, "instance_id": req.InstanceID}).One(&item); err != nil {
			return err
		}
		if item.State != "restart_ready" || item.SourceGeneration == nil || req.Generation != *item.SourceGeneration+1 || item.PostflightJSON == nil || item.SnapshotRef == nil {
			return ErrUpgradeReservationDenied
		}
		if item.IsolatedAt != nil || (item.RecoveryStatus != nil && *item.RecoveryStatus == "needs_attention") {
			return ErrUpgradeRecoveryHeld
		}
		if item.RestartTargetPodUID != nil && *item.RestartTargetPodUID != *identity {
			// A missing binding does not prove an old writer on another pod is
			// gone. Changing writer identity needs separate fenced reconciliation.
			return ErrUpgradeReservationDenied
		}
		var instance struct {
			Status     string `db:"status"`
			Generation int    `db:"runtime_generation"`
			Type       string `db:"type"`
			Mode       string `db:"instance_mode"`
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id = ? FOR UPDATE", req.InstanceID); err != nil {
			return err
		}
		if err := tx.SQL().Select("status", "runtime_generation", "type", "instance_mode").From("instances").Where("id", req.InstanceID).One(&instance); err != nil {
			return err
		}
		if instance.Generation != req.Generation || instance.Type != "openclaw" || instance.Mode != "lite" || (instance.Status != "creating" && instance.Status != "running") {
			return ErrUpgradeReservationDenied
		}
		var pod models.RuntimePod
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_pods WHERE id = ? FOR UPDATE", req.Pod.ID); err != nil {
			return err
		}
		if err := tx.Collection("runtime_pods").Find("id", req.Pod.ID).One(&pod); err != nil {
			return err
		}
		if (pod.PodUID != nil && *pod.PodUID != *identity) || (pod.PodUID == nil && req.Pod.KubernetesPodUID == nil) || pod.PodName != req.Pod.PodName || pod.Namespace != req.Pod.Namespace || pod.DeploymentName != req.Pod.DeploymentName || pod.RuntimeType != "openclaw" || pod.State != "ready" || pod.Draining || pod.ImageDigest == nil || *pod.ImageDigest != *rollout.TargetImageDigest || pod.AgentEndpoint == nil || req.Pod.AgentEndpoint == nil || *pod.AgentEndpoint != *req.Pod.AgentEndpoint {
			return ErrUpgradeReservationDenied
		}
		var sources map[string]string
		if rollout.SourceImagesJSON == nil || json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources) != nil {
			return ErrUpgradeReservationDenied
		}
		owned := false
		for source := range sources {
			parts := strings.SplitN(source, "/", 2)
			if len(parts) == 2 && parts[0] == pod.Namespace && upgradeTargetDeploymentName(parts[1], rollout.ID) == pod.DeploymentName {
				owned = true
			}
		}
		if !owned {
			return ErrUpgradeReservationDenied
		}
		// Another active upgrade of the same type is never implicitly authorized.
		var other int
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_rollouts WHERE runtime_type = 'openclaw' AND status IN ('pending','running') AND id <> ?", rollout.ID)
		if err != nil {
			return err
		}
		if err := row.Scan(&other); err != nil {
			return err
		}
		if other != 0 {
			return ErrUpgradeReservationDenied
		}
		var existing models.InstanceRuntimeBinding
		err = tx.Collection("instance_runtime_bindings").Find("instance_id", req.InstanceID).One(&existing)
		if err == nil {
			if existing.RuntimePodID != pod.ID || existing.Generation != req.Generation || existing.WorkspacePath != req.WorkspacePath {
				return ErrUpgradeReservationDenied
			}
			binding = &existing // no second capacity claim or attempt
			return nil
		}
		if err != db.ErrNoMoreRows {
			return err
		}
		if item.RestartAttempts >= UpgradeRestartMaxAttempts {
			return ErrUpgradeRecoveryHeld
		}
		if item.RestartNextRetryAt != nil && time.Now().UTC().Before(*item.RestartNextRetryAt) {
			return ErrUpgradeCapacityPending
		}
		var bindings []models.InstanceRuntimeBinding
		if err := tx.Collection("instance_runtime_bindings").Find("runtime_pod_id", pod.ID).All(&bindings); err != nil {
			return err
		}
		// Heartbeats report actual processes, not in-flight reservations. Counting
		// durable bindings here prevents a fresh zero-process heartbeat from
		// erasing capacity already reserved by this upgrade.
		used := pod.UsedSlots
		if len(bindings) > used {
			used = len(bindings)
		}
		if used >= pod.Capacity {
			return ErrUpgradeCapacityPending
		}
		port := 0
		for p := req.PortStart; p+req.PortBlockSize-1 <= req.PortEnd; p += req.PortBlockSize {
			free := true
			for _, b := range bindings {
				if b.GatewayPort <= p+req.PortBlockSize-1 && b.GatewayPort+req.PortBlockSize-1 >= p {
					free = false
					break
				}
			}
			if free {
				port = p
				break
			}
		}
		if port == 0 {
			return ErrUpgradeCapacityPending
		}
		now := time.Now().UTC()
		binding = &models.InstanceRuntimeBinding{InstanceID: req.InstanceID, RuntimePodID: pod.ID, RuntimeType: "openclaw", GatewayID: fmt.Sprintf("pending-%d-%d", req.InstanceID, req.Generation), GatewayPort: port, WorkspacePath: req.WorkspacePath, State: "creating", Generation: req.Generation, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.Collection("instance_runtime_bindings").Insert(binding); err != nil {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET used_slots = ?, updated_at = ? WHERE id = ?", used+1, now, pod.ID); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_started_at = COALESCE(recovery_started_at, ?), restart_attempts = restart_attempts + 1, restart_target_pod_uid = ?, restart_started_at = ?, restart_deadline_at = ?, restart_next_retry_at = NULL, recovery_status = 'starting', recovery_error_code = NULL, error_message = NULL WHERE id = ?`, now, *identity, now, now.Add(UpgradeRestartTimeout), item.ID)
		if err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"instance_id": req.InstanceID, "generation": req.Generation, "pod_uid": *identity, "attempt": item.RestartAttempts + 1})
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO runtime_upgrade_audits (rollout_id,action,phase,outcome,detail_json,created_at) VALUES (?,'gateway_start_reserved','gateway_restart','accepted',?,?)`, rollout.ID, string(detail), now)
		return err
	}, nil)
	return binding, err
}

func upgradeTargetDeploymentName(source string, rolloutID int64) string {
	suffix := fmt.Sprintf("-u%d", rolloutID)
	source = strings.Trim(strings.ToLower(strings.TrimSpace(source)), "-")
	if len(source) > 63-len(suffix) {
		source = strings.TrimRight(source[:63-len(suffix)], "-")
	}
	return source + suffix
}

func lockUpgradeRow(ctx context.Context, tx db.Session, query string, args ...any) error {
	row, err := tx.SQL().QueryRowContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var id int64
	return row.Scan(&id)
}
