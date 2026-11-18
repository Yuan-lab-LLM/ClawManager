package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Absence is only suspicion. This marker keeps state, health time, assignment and
// capacity intact; source teardown still requires the normal exact stop proof.
type ProtectedGatewayMissingReportMarker interface {
	MarkProtectedGatewayReportMissing(context.Context, models.InstanceRuntimeBinding, time.Time, string) (bool, error)
}

func (r *instanceRuntimeBindingRepository) MarkProtectedGatewayReportMissing(ctx context.Context, binding models.InstanceRuntimeBinding, before time.Time, message string) (bool, error) {
	marked := false
	err := (&instanceRepository{r.sess}).withUnfencedInstance(ctx, binding.InstanceID, func(local *instanceRepository) error {
		result, err := local.sess.SQL().ExecContext(ctx, `UPDATE instance_runtime_bindings SET error_message=? WHERE instance_id=? AND runtime_pod_id=? AND generation=? AND CAST(gateway_id AS BINARY)=CAST(? AS BINARY) AND state=? AND updated_at<=? AND (last_health_at IS NULL OR last_health_at<=?)`, message, binding.InstanceID, binding.RuntimePodID, binding.Generation, binding.GatewayID, binding.State, before, before)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		marked = n > 0
		return err
	})
	return marked, err
}

// The caller must first verify current SQL intent, assignment and Ready Pod
// authority. An ordinary report or a previous-generation observation is not
// authority to reconstruct an unbound protected writer.
type UnboundProtectedReportedGatewayReconciler interface {
	ReconcileUnboundProtectedReportedGateway(context.Context, *models.InstanceRuntimeBinding, int, string, int) (bool, error)
}

func (r *instanceRuntimeBindingRepository) ReconcileUnboundProtectedReportedGateway(ctx context.Context, binding *models.InstanceRuntimeBinding, owner int, podUID string, block int) (bool, error) {
	if binding == nil || binding.InstanceID < 1 || binding.RuntimePodID < 1 || owner < 1 || podUID == "" || binding.Generation < 1 || block < 1 || binding.GatewayPort < 1024 || binding.GatewayPort > 65535-block+1 || binding.GatewayID != fmt.Sprintf("gw-%d-%d", binding.InstanceID, binding.Generation) || binding.WorkspacePath == "" || binding.State != "running" {
		return false, nil
	}
	reconciled := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var current models.Instance
		var workspace string
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id,user_id,type,instance_mode,runtime_type,status,runtime_generation,runtime_security_profile,COALESCE(workspace_path,'') FROM instances WHERE id=? FOR UPDATE`, binding.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&current.ID, &current.UserID, &current.Type, &current.InstanceMode, &current.RuntimeType, &current.Status, &current.RuntimeGeneration, &current.RuntimeSecurityProfile, &workspace); err != nil {
			return err
		}
		if workspace != binding.WorkspacePath || current.UserID != owner || current.Type != binding.RuntimeType || current.InstanceMode != "lite" || current.RuntimeType != "gateway" || current.RuntimeSecurityProfile != "isolated-network-v1" || current.RuntimeGeneration != binding.Generation || (current.Status != "running" && current.Status != "creating") {
			return nil
		}
		if err := checkRuntimeUpgrade(ctx, tx, binding.InstanceID); err != nil {
			return err
		}
		if err := CheckRuntimePodBindingAdmission(ctx, tx, binding.RuntimePodID); err != nil {
			return err
		}
		// Keep the instance lock ahead of consistent reads, and use current
		// intent reads so a concurrent stop cannot be hidden by a prior snapshot.
		var blockers int
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_desired_state WHERE instance_id=? AND CAST(COALESCE(desired_power_state,'') AS BINARY)<>CAST('running' AS BINARY) FOR UPDATE`, binding.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&blockers); err != nil {
			return err
		}
		if blockers != 0 {
			return nil
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_model_activity WHERE instance_id=? AND (stop_operation_id<>'' OR (start_deadline_at IS NOT NULL AND start_deadline_at<=?)) FOR UPDATE`, binding.InstanceID, time.Now().UTC())
		if err != nil {
			return err
		}
		if err = row.Scan(&blockers); err != nil {
			return err
		}
		if blockers != 0 {
			return nil
		}
		var uid, kind string
		var capacity int
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(pod_uid,''),runtime_type,capacity FROM runtime_pods WHERE id=? FOR UPDATE`, binding.RuntimePodID)
		if err != nil {
			return err
		}
		if err = row.Scan(&uid, &kind, &capacity); err != nil {
			return err
		}
		if uid != podUID || kind != binding.RuntimeType || capacity < 1 {
			return nil
		}
		var existing, conflicts, occupied int
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=? FOR UPDATE`, binding.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&existing); err != nil {
			return err
		}
		if existing != 0 {
			return nil
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(gateway_port<=? AND gateway_port+?-1>=?),0) FROM instance_runtime_bindings WHERE runtime_pod_id=?`, binding.GatewayPort+block-1, block, binding.GatewayPort, binding.RuntimePodID)
		if err != nil {
			return err
		}
		if err = row.Scan(&occupied, &conflicts); err != nil {
			return err
		}
		if conflicts != 0 || occupied >= capacity {
			return nil
		}
		now := time.Now().UTC()
		if _, err = tx.SQL().ExecContext(ctx, `INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,gateway_pid,workspace_path,state,generation,last_health_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,'running',?,?,?,?)`, binding.InstanceID, binding.RuntimePodID, binding.RuntimeType, binding.GatewayID, binding.GatewayPort, binding.GatewayPID, binding.WorkspacePath, binding.Generation, binding.LastHealthAt, now, now); err != nil {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instances SET status='running',runtime_error_message=NULL,runtime_error_code=NULL,updated_at=? WHERE id=?`, now, binding.InstanceID); err != nil {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(used_slots,?),updated_at=? WHERE id=?`, occupied+1, now, binding.RuntimePodID); err != nil {
			return err
		}
		reconciled = true
		return nil
	}, nil)
	return reconciled, err
}
