package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"errors"
	"time"
)

// Reuse the confirmed-stop intent; no second controller or parallel task state.
func (r *InstanceAutoStopRepository) RestartFollowup(ctx context.Context, instanceID int) (*models.InstanceIdleStopOperation, error) {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,error_message,created_at,stop_kind FROM instance_idle_stop_operations WHERE instance_id=? AND stop_kind='restart' AND phase IN ('restart_ready','restart_blocked') ORDER BY created_at DESC LIMIT 1`, instanceID)
	if err != nil {
		return nil, err
	}
	var op models.InstanceIdleStopOperation
	err = row.Scan(&op.ID, &op.InstanceID, &op.UserID, &op.Generation, &op.RuntimePodID, &op.GatewayID, &op.PodUID, &op.Endpoint, &op.WorkspacePath, &op.StorageIdentity, &op.Phase, &op.ErrorMessage, &op.CreatedAt, &op.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &op, err
}

func (r *InstanceAutoStopRepository) FinishRestartFollowup(ctx context.Context, op models.InstanceIdleStopOperation, cause error) error {
	phase, message := "completed", ""
	if cause != nil {
		phase, message = "restart_blocked", "restart start was not accepted; retry Restart or cancel with Stop"
	}
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET phase=?,error_message=?,updated_at=? WHERE id=? AND stop_kind='restart' AND phase IN ('restart_ready','restart_blocked')`, phase, message, time.Now().UTC(), op.ID)
	return err
}

func (r *InstanceAutoStopRepository) GetStopOperation(ctx context.Context, id string) (*models.InstanceIdleStopOperation, error) {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,error_message,created_at,stop_kind FROM instance_idle_stop_operations WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	var op models.InstanceIdleStopOperation
	err = row.Scan(&op.ID, &op.InstanceID, &op.UserID, &op.Generation, &op.RuntimePodID, &op.GatewayID, &op.PodUID, &op.Endpoint, &op.WorkspacePath, &op.StorageIdentity, &op.Phase, &op.ErrorMessage, &op.CreatedAt, &op.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &op, err
}
