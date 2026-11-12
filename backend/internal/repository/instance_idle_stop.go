package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
)

func (r *InstanceAutoStopRepository) ClaimStop(ctx context.Context, settingsVersion, activityVersion int64, input models.InstanceIdleStopOperation) (*models.InstanceIdleStopOperation, error) {
	if _, err := r.Settings(ctx); err != nil {
		return nil, err
	}
	var claimed *models.InstanceIdleStopOperation
	err := r.sess.Tx(func(s db.Session) error {
		cfg, err := readAutoStopSettings(ctx, s, true)
		if err != nil {
			return err
		}
		if !cfg.Enabled || cfg.Version != settingsVersion {
			return models.ErrAutoStopConflict
		}
		i, err := lockActivityInstance(ctx, s, input.InstanceID)
		if err != nil {
			return err
		}
		if i.mode != "lite" || i.status != "running" || i.generation != input.Generation || i.userID != input.UserID {
			return models.ErrAutoStopConflict
		}
		if err := requireTeamStopAuthority(ctx, s, input.InstanceID, input.Generation); err != nil {
			return err
		}
		a, err := readActivity(ctx, s, input.InstanceID, true)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if a == nil || a.Generation != i.generation || a.ActivityVersion != activityVersion {
			return models.ErrAutoStopConflict
		}
		if a.ActiveRequests != 0 {
			row, err := s.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM instance_model_activity_requests WHERE instance_id=? AND generation=? AND finished_at IS NULL AND lease_expires_at IS NULL)`, input.InstanceID, input.Generation)
			if err != nil {
				return err
			}
			var legacy bool
			if err = row.Scan(&legacy); err != nil {
				return err
			}
			if legacy {
				return models.ErrLegacyModelActivityUnverified
			}
			return models.ErrModelActivityInProgress
		}
		if !a.IdleAtDuration(now, cfg.IdleTimeout()) {
			return models.ErrAutoStopConflict
		}
		workRow, err := s.SQL().QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?) +
 (SELECT COUNT(*) FROM instance_commands WHERE instance_id=? AND status IN ('pending','queued','accepted','batch_pending','dispatched','running')) +
 (SELECT COUNT(*) FROM gateway_runtime_tasks WHERE instance_id=? AND generation=? AND finished=FALSE)`, input.InstanceID, input.InstanceID, input.InstanceID, input.Generation)
		if err != nil {
			return err
		}
		var openWork int
		if err = workRow.Scan(&openWork); err != nil {
			return err
		}
		if openWork != 0 {
			return fmt.Errorf("outstanding platform tasks or workspace writes block automatic stop")
		}
		row, err := s.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=? AND runtime_pod_id=? AND gateway_id=? AND generation=? AND state IN ('running','ready','healthy')`, input.InstanceID, input.RuntimePodID, input.GatewayID, input.Generation)
		if err != nil {
			return err
		}
		var n int
		if err = row.Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return models.ErrAutoStopConflict
		}
		// A restart after observation cannot shorten the mandatory idle window.
		row, err = s.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances WHERE id=? AND (started_at IS NULL OR started_at<=?)`, input.InstanceID, now.Add(-cfg.IdleTimeout()))
		if err != nil {
			return err
		}
		if err = row.Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return models.ErrAutoStopConflict
		}
		input.ID = uuid.NewString()
		input.Kind = "idle"
		input.Phase = "stopping"
		input.CreatedAt = now
		_, err = s.SQL().ExecContext(ctx, `INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.ID, input.InstanceID, input.UserID, input.Generation, input.RuntimePodID, input.GatewayID, input.PodUID, input.Endpoint, input.WorkspacePath, input.StorageIdentity, input.Phase, now, now)
		if err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stop_operation_id=?,stop_phase='stopping',blocker='',updated_at=? WHERE instance_id=?`, input.ID, now, input.InstanceID)
		if err != nil {
			return err
		}
		// Persist the desired stop before contacting Runtime. This removes the
		// instance from scheduler/failover desired-running queries after a crash.
		if err = revokeStoppedInstanceAuthority(ctx, s, input.InstanceID); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instances SET status='stopped',updated_at=? WHERE id=? AND runtime_generation=?`, now, input.InstanceID, input.Generation)
		if err == nil {
			claimed = &input
		}
		return err
	})
	return claimed, err
}
func (r *InstanceAutoStopRepository) CompleteStop(ctx context.Context, op models.InstanceIdleStopOperation) error {
	return r.sess.Tx(func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, op.InstanceID)
		if err != nil {
			return err
		}
		a, err := readActivity(ctx, s, op.InstanceID, true)
		if err != nil {
			return err
		}
		if a == nil || a.StopOperationID != op.ID || i.generation != op.Generation || i.status != "stopped" {
			return models.ErrAutoStopConflict
		}
		if a.StopPhase != "teardown_confirmed" {
			return fmt.Errorf("graceful teardown proof has not been persisted")
		}
		row, err := s.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?`, op.InstanceID)
		if err != nil {
			return err
		}
		var n int
		if err = row.Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("runtime binding remains; confirmed teardown must release it before completion")
		}
		now := time.Now().UTC()
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET phase='completed',error_message='',updated_at=? WHERE id=? AND generation=?`, now, op.ID, op.Generation)
		if err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stop_operation_id='',stop_phase='completed',stopped_reason='idle_timeout',stopped_at=?,blocker='',updated_at=? WHERE instance_id=? AND stop_operation_id=?`, now, now, op.InstanceID, op.ID)
		if err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instances SET stopped_at=?,updated_at=? WHERE id=? AND runtime_generation=?`, now, now, op.InstanceID, op.Generation)
		return err
	})
}
func (r *InstanceAutoStopRepository) FailStop(ctx context.Context, op models.InstanceIdleStopOperation) error {
	// Keep the fence and exact source, even after an HTTP timeout or controller
	// restart. Recovery is allowed only through the graceful idle-stop protocol.
	return r.sess.Tx(func(s db.Session) error {
		if _, err := lockActivityInstance(ctx, s, op.InstanceID); err != nil {
			return err
		}
		now := time.Now().UTC()
		msg := op.ErrorMessage
		if len(msg) > 500 {
			msg = msg[:500]
		}
		_, err := s.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET phase=IF(phase='teardown_confirmed',phase,'error'),error_message=?,updated_at=? WHERE id=? AND phase<>'completed'`, msg, now, op.ID)
		if err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stop_phase=IF(stop_phase='teardown_confirmed',stop_phase,'error'),blocker=?,updated_at=? WHERE instance_id=? AND stop_operation_id=?`, msg, now, op.InstanceID, op.ID)
		return err
	})
}
func (r *InstanceAutoStopRepository) PendingStops(ctx context.Context) ([]models.InstanceIdleStopOperation, error) {
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,error_message,created_at,stop_kind FROM instance_idle_stop_operations WHERE phase IN ('stopping','error','teardown_confirmed','restart_ready') ORDER BY updated_at,created_at LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.InstanceIdleStopOperation{}
	for rows.Next() {
		var op models.InstanceIdleStopOperation
		if err = rows.Scan(&op.ID, &op.InstanceID, &op.UserID, &op.Generation, &op.RuntimePodID, &op.GatewayID, &op.PodUID, &op.Endpoint, &op.WorkspacePath, &op.StorageIdentity, &op.Phase, &op.ErrorMessage, &op.CreatedAt, &op.Kind); err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}
func (r *InstanceAutoStopRepository) RecordManualStop(ctx context.Context, id int) error {
	return r.sess.Tx(func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		if i.mode != "lite" {
			return nil
		}
		now := time.Now().UTC()
		if err = ensureActivity(ctx, s, id, i.generation, now); err != nil {
			return err
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a.StopOperationID != "" {
			return models.ErrInstanceStopping
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stopped_reason='manual',stopped_at=?,stop_operation_id='',stop_phase='completed',blocker='',start_deadline_at=NULL,updated_at=? WHERE instance_id=?`, now, now, id)
		return err
	})
}
func (r *InstanceAutoStopRepository) BeginManualStart(ctx context.Context, id int, deadline time.Time) error {
	return r.sess.Tx(func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		if i.status == "deleting" {
			return models.ErrInstanceStopping
		}
		if i.mode != "lite" {
			return nil
		}
		if i.status == "creating" {
			return fmt.Errorf("%w: instance Start is already pending", models.ErrAutoStopConflict)
		}
		now := time.Now().UTC()
		if err = ensureActivity(ctx, s, id, i.generation, now); err != nil {
			return err
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a.StartDeadlineAt != nil {
			return fmt.Errorf("%w: instance Start is already pending", models.ErrAutoStopConflict)
		}
		if a.StopOperationID != "" {
			return models.ErrInstanceStopping
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET monitoring_since=?,start_deadline_at=?,blocker='',updated_at=? WHERE instance_id=?`, now, deadline.UTC(), now, id)
		if err != nil {
			return err
		}
		// Publish the explicit Start intent under the same instance row lock as
		// Stop. A later committed Stop must still revoke publication authority.
		_, err = s.SQL().ExecContext(ctx, `INSERT INTO instance_desired_state(instance_id,desired_power_state,created_at,updated_at) VALUES(?,'running',?,?) ON DUPLICATE KEY UPDATE desired_power_state='running',updated_at=VALUES(updated_at)`, id, now, now)
		return err
	})
}
func (r *InstanceAutoStopRepository) ExpiredStarts(ctx context.Context, now time.Time) ([]int, error) {
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT a.instance_id FROM instance_model_activity a JOIN instances i ON i.id=a.instance_id WHERE a.start_deadline_at IS NOT NULL AND a.start_deadline_at<=? AND i.instance_mode='lite' AND i.status IN ('creating','error','stopped') ORDER BY a.instance_id LIMIT 100`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (r *InstanceAutoStopRepository) ClearStartDeadline(ctx context.Context, id int) error {
	return r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		if i.mode != "lite" {
			return nil
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a == nil || a.StartDeadlineAt == nil {
			return nil
		}
		now := time.Now().UTC()
		if i.status != "running" {
			if err = revokeStoppedInstanceAuthority(ctx, s, id); err != nil {
				return err
			}
			if _, err = s.SQL().ExecContext(ctx, `UPDATE instances SET status='stopped',updated_at=? WHERE id=?`, now, id); err != nil {
				return err
			}
			if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stopped_reason=IF(stopped_reason='','manual',stopped_reason) WHERE instance_id=?`, id); err != nil {
				return err
			}
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET start_deadline_at=NULL,updated_at=? WHERE instance_id=?`, now, id)
		return err
	}, nil)
}

// The caller has confirmed the original failed Start's native teardown under
// the lifecycle operation lock. Recheck its identity and admission atomically;
// late proof must never clear a subsequent Start or an in-progress Stop.
func (r *InstanceAutoStopRepository) ClearExpiredStartDeadline(ctx context.Context, id, generation int, deadline time.Time) error {
	return r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a == nil || a.StartDeadlineAt == nil {
			return nil
		}
		now := time.Now().UTC()
		if i.mode != "lite" || i.status != "stopped" || i.generation != generation || a.StopOperationID != "" || !a.StartDeadlineAt.Equal(deadline) || deadline.After(now) {
			return models.ErrAutoStopConflict
		}
		row, err := s.SQL().QueryRowContext(ctx, `SELECT
		 (SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?)+
		 (SELECT COUNT(*) FROM instance_model_activity_requests WHERE instance_id=? AND finished_at IS NULL AND (lease_expires_at IS NULL OR lease_expires_at>UTC_TIMESTAMP(6)))+
		 (SELECT COUNT(*) FROM gateway_runtime_tasks WHERE instance_id=? AND generation=? AND finished=FALSE)`, id, id, id, generation)
		if err != nil {
			return err
		}
		var active int
		if err = row.Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return models.ErrModelActivityInProgress
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET generation=?,start_deadline_at=NULL,blocker='',activity_version=activity_version+1,updated_at=? WHERE instance_id=?`, generation, now, id)
		return err
	}, nil)
}

// Preserve proof before releasing a binding: recovery must not contact a slot
// that may already have been assigned to a different instance.
func (r *InstanceAutoStopRepository) ConfirmTeardown(ctx context.Context, op models.InstanceIdleStopOperation) error {
	return r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, op.InstanceID)
		if err != nil {
			return err
		}
		a, err := readActivity(ctx, s, op.InstanceID, true)
		if err != nil {
			return err
		}
		if a == nil || a.StopOperationID != op.ID || i.generation != op.Generation || i.status != "stopped" {
			return models.ErrAutoStopConflict
		}
		now := time.Now().UTC()
		if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET phase='teardown_confirmed',error_message='',updated_at=? WHERE id=? AND phase IN ('stopping','error','teardown_confirmed')`, now, op.ID); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stop_phase='teardown_confirmed',blocker='',updated_at=? WHERE instance_id=? AND stop_operation_id=?`, now, op.InstanceID, op.ID)
		return err
	}, nil)
}
func (r *InstanceAutoStopRepository) FenceExpiredStart(ctx context.Context, id int) (bool, error) {
	var changed bool
	err := r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if i.mode != "lite" || (i.status != "creating" && i.status != "error" && i.status != "stopped") || a == nil || a.StartDeadlineAt == nil || a.StartDeadlineAt.After(time.Now()) {
			return nil
		}
		if err = revokeStoppedInstanceAuthority(ctx, s, id); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instances SET status='stopped',runtime_error_message='Start timed out waiting for capacity or readiness; retry Start manually',updated_at=? WHERE id=?`, time.Now().UTC(), id)
		if err == nil {
			_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stopped_reason=IF(stopped_reason='','manual',stopped_reason),blocker='Start timed out waiting for capacity or readiness; retry Start manually',updated_at=? WHERE instance_id=?`, time.Now().UTC(), id)
		}
		changed = err == nil
		return err
	}, nil)
	return changed, err
}

// The original durable backing remains binding across subsequent explicit
// restarts; an unrelated pool must never create an empty replacement home.
// PreviousIdleStorage also recognizes manual stops whose empty Pod was later
// removed by automatic scale-in. The exact Pod UID links the stop receipt to a
// verified persistent snapshot, without delaying durable Stop intent or rewriting
// completed stop records.
func (r *InstanceAutoStopRepository) PreviousIdleStorage(ctx context.Context, id int) (string, error) {
	// A Stop before a pending Start obtains a binding has no new storage proof.
	// Such receipts must not erase an earlier verified persistent home.
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT storage_identity FROM instance_idle_stop_operations WHERE instance_id=? AND phase='completed' AND stop_kind IN ('idle','manual') AND storage_identity<>'' ORDER BY created_at DESC LIMIT 1`, id)
	if err != nil {
		return "", err
	}
	var identity string
	err = row.Scan(&identity)
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(m.request_json,'$.snapshot.WorkspaceStorageIdentity')),'') FROM instance_idle_stop_operations s JOIN runtime_management_operations m ON m.kind='scale_in' AND m.status='completed' AND JSON_EXTRACT(m.request_json,'$.automatic_idle')=true AND JSON_CONTAINS(JSON_EXTRACT(m.request_json,'$.plan.source_pods'),JSON_OBJECT('id',s.runtime_pod_id,'pod_uid',s.pod_uid)) WHERE s.instance_id=? AND s.phase='completed' AND s.stop_kind IN ('idle','manual') AND s.runtime_pod_id>0 AND s.pod_uid<>'' ORDER BY s.created_at DESC,m.created_at DESC LIMIT 1`, id)
	if err != nil {
		return "", err
	}
	err = row.Scan(&identity)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return identity, err
}

// Manual cleanup has the same durable identity and fence as idle cleanup, but
// only this explicitly requested mode may invoke the existing delete protocol.
func (r *InstanceAutoStopRepository) BeginManualStop(ctx context.Context, input models.InstanceIdleStopOperation) (*models.InstanceIdleStopOperation, error) {
	if input.Kind != "restart" {
		input.Kind = "manual"
	}
	var result *models.InstanceIdleStopOperation
	err := r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, input.InstanceID)
		if err != nil {
			return err
		}
		if i.status == "deleting" {
			return models.ErrInstanceStopping
		}
		if i.mode != "lite" || i.generation != input.Generation || i.userID != input.UserID {
			return models.ErrAutoStopConflict
		}
		now := time.Now().UTC()
		// An explicit Stop cancels only an unstarted restart follow-up. The old
		// source still goes through its original confirmed teardown operation.
		if input.Kind == "manual" {
			if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET stop_kind='manual',phase=IF(phase IN ('restart_ready','restart_blocked'),'completed',phase),updated_at=? WHERE instance_id=? AND stop_kind='restart' AND phase<>'completed'`, now, input.InstanceID); err != nil {
				return err
			}
		}
		if err = ensureActivity(ctx, s, input.InstanceID, i.generation, now); err != nil {
			return err
		}
		a, err := readActivity(ctx, s, input.InstanceID, true)
		if err != nil {
			return err
		}
		if a.StopOperationID != "" {
			row, e := s.SQL().QueryRowContext(ctx, `SELECT id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,error_message,created_at,stop_kind FROM instance_idle_stop_operations WHERE id=?`, a.StopOperationID)
			if e != nil {
				return e
			}
			var old models.InstanceIdleStopOperation
			if e = row.Scan(&old.ID, &old.InstanceID, &old.UserID, &old.Generation, &old.RuntimePodID, &old.GatewayID, &old.PodUID, &old.Endpoint, &old.WorkspacePath, &old.StorageIdentity, &old.Phase, &old.ErrorMessage, &old.CreatedAt, &old.Kind); e != nil {
				return e
			}
			if old.Kind != "manual" && old.Kind != "restart" {
				return models.ErrInstanceStopping
			}
			if input.Kind == "restart" && old.Kind != "restart" {
				return models.ErrInstanceStopping
			}
			result = &old
			return nil
		}
		input.ID = uuid.NewString()
		input.Phase = "stopping"
		input.CreatedAt = now
		_, err = s.SQL().ExecContext(ctx, `INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.ID, input.InstanceID, input.UserID, input.Generation, input.RuntimePodID, input.GatewayID, input.PodUID, input.Endpoint, input.WorkspacePath, input.StorageIdentity, input.Phase, input.Kind, now, now)
		if err != nil {
			return err
		}
		if err = revokeStoppedInstanceAuthority(ctx, s, input.InstanceID); err != nil {
			return err
		}
		if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stopped_reason='manual',stop_operation_id=?,stop_phase='stopping',blocker='',start_deadline_at=NULL,updated_at=? WHERE instance_id=?`, input.ID, now, input.InstanceID); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instances SET status='stopped',updated_at=? WHERE id=?`, now, input.InstanceID)
		if err == nil {
			result = &input
		}
		return err
	}, nil)
	return result, err
}

func (r *InstanceAutoStopRepository) CompleteManualStop(ctx context.Context, op models.InstanceIdleStopOperation) error {
	return r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, op.InstanceID)
		if err != nil {
			return err
		}
		a, err := readActivity(ctx, s, op.InstanceID, true)
		if err != nil {
			return err
		}
		if (op.Kind != "manual" && op.Kind != "restart") || a == nil || a.StopOperationID != op.ID || a.StopPhase != "teardown_confirmed" || i.generation != op.Generation || i.status != "stopped" {
			return models.ErrAutoStopConflict
		}
		row, err := s.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?`, op.InstanceID)
		if err != nil {
			return err
		}
		var n int
		if err = row.Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return models.ErrInstanceStopping
		}
		now := time.Now().UTC()
		phase := "completed"
		if op.Kind == "restart" {
			phase = "restart_ready"
		}
		if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET phase=?,error_message='',updated_at=? WHERE id=? AND stop_kind=?`, phase, now, op.ID, op.Kind); err != nil {
			return err
		}
		if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stopped_reason='manual',stopped_at=?,stop_operation_id='',stop_phase='completed',blocker='',updated_at=? WHERE instance_id=?`, now, now, op.InstanceID); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instances SET stopped_at=?,updated_at=? WHERE id=?`, now, now, op.InstanceID)
		return err
	}, nil)
}

func revokeStoppedInstanceAuthority(ctx context.Context, s db.Session, id int) error {
	now := time.Now().UTC()
	if _, err := s.SQL().ExecContext(ctx, `UPDATE instances SET agent_bootstrap_token=NULL WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := s.SQL().ExecContext(ctx, `UPDATE instance_agents SET status='offline',session_token=NULL,session_expires_at=NULL,updated_at=? WHERE instance_id=?`, now, id); err != nil {
		return err
	}
	_, err := s.SQL().ExecContext(ctx, `INSERT INTO instance_desired_state(instance_id,desired_power_state,created_at,updated_at) VALUES(?,'stopped',?,?) ON DUPLICATE KEY UPDATE desired_power_state='stopped',updated_at=VALUES(updated_at)`, id, now, now)
	return err
}

// Fairness must not depend on acquiring the instance/upgrade lock. A blocked
// source retains its fence but moves behind other pending recovery attempts.
func (r *InstanceAutoStopRepository) TouchStopRetry(ctx context.Context, id string) error {
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET updated_at=? WHERE id=? AND phase IN ('stopping','error','teardown_confirmed')`, time.Now().UTC(), id)
	return err
}
