package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// ReleaseUnpublishedFirstReservation rolls back a reservation, never a writer.
// The caller holds the instance lifecycle lock. Publication and this rollback
// serialize on the same admission row; verify must freshly establish native
// no-publication and original Pod absence while that fence is held.
func (r *instanceRuntimeBindingRepository) ReleaseUnpublishedFirstReservation(ctx context.Context, instance models.Instance, binding models.InstanceRuntimeBinding, pod models.RuntimePod, verify func() error) (bool, error) {
	if verify == nil || instance.ID < 1 || instance.UserID < 1 || instance.RuntimeGeneration != 1 || instance.Status != "creating" || instance.InstanceMode != "lite" || instance.RuntimeType != "gateway" || binding.ID < 1 || binding.InstanceID != instance.ID || binding.RuntimePodID != pod.ID || pod.ID < 1 || binding.Generation != 1 || binding.State != "creating" || binding.GatewayPID != nil || binding.GatewayID != fmt.Sprintf("pending-%d-1", instance.ID) || binding.RuntimeType != instance.Type || pod.RuntimeType != instance.Type || pod.PodUID == nil || *pod.PodUID == "" || pod.AgentEndpoint == nil || *pod.AgentEndpoint == "" {
		return false, fmt.Errorf("rollback requires an exact first-generation pending reservation")
	}
	released := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		// This sticky marker is written only after publication, even if native credentials were later lost.
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=? AND user_id=? AND runtime_generation=1 AND BINARY status=BINARY 'creating' AND BINARY type=BINARY ? AND BINARY runtime_type=BINARY 'gateway' AND BINARY instance_mode=BINARY 'lite' AND BINARY runtime_security_profile=BINARY '' FOR UPDATE`, instance.ID, instance.UserID, instance.Type)
		if err != nil {
			return err
		}
		var id int
		if err = row.Scan(&id); err != nil {
			return err
		}
		if err = checkRuntimeUpgrade(ctx, tx, instance.ID); err != nil {
			return err
		}
		// Any retained execution/recovery ownership makes no-start inference unsafe.
		for _, query := range []string{
			"SELECT COUNT(*) FROM instance_lifecycle_manual_guards WHERE instance_id=?",
			"SELECT COUNT(*) FROM instance_runtime_recovery WHERE instance_id=?",
			"SELECT COUNT(*) FROM instance_idle_stop_operations WHERE instance_id=? AND runtime_pod_id<>0",
			"SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?",
			"SELECT COUNT(*) FROM team_members WHERE instance_id=? AND status NOT IN ('deleted','deleting')",
			"SELECT COUNT(*) FROM runtime_management_operations WHERE JSON_CONTAINS(items_json,JSON_OBJECT('instance_id',?), '$')",
		} {
			row, err = tx.SQL().QueryRowContext(ctx, query, instance.ID)
			if err != nil {
				return err
			}
			var count int
			if err = row.Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("pending reservation has retained lifecycle or writer history")
			}
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT id FROM instance_runtime_bindings WHERE id=? AND instance_id=? AND runtime_pod_id=? AND generation=1 AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ? AND BINARY state=BINARY 'creating' AND gateway_pid IS NULL FOR UPDATE`, binding.ID, instance.ID, pod.ID, binding.GatewayID, binding.GatewayPort, binding.RuntimeType, binding.WorkspacePath)
		if err != nil {
			return err
		}
		if err = row.Scan(&id); err != nil {
			return err
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT id FROM runtime_pods WHERE id=? AND BINARY pod_uid=BINARY ? AND BINARY namespace=BINARY ? AND BINARY pod_name=BINARY ? AND BINARY runtime_type=BINARY ? AND BINARY agent_endpoint=BINARY ? FOR UPDATE`, pod.ID, *pod.PodUID, pod.Namespace, pod.PodName, pod.RuntimeType, *pod.AgentEndpoint)
		if err != nil {
			return err
		}
		if err = row.Scan(&id); err != nil {
			return err
		}
		// Heartbeat failover can journal this same reservation before first
		// publication. Lock its row so a delayed stop result cannot race the
		// no-start proof. Begin/Advance share the admission lock above; Save is
		// an exact UPDATE and cannot recreate a consumed intent.
		var failover models.RuntimeFailoverIntent
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT instance_id,user_id,generation,runtime_type,source_pod_id,source_pod_uid,source_endpoint,gateway_id,gateway_port,workspace_path,source_stopped FROM runtime_failover_intents WHERE instance_id=? FOR UPDATE`, instance.ID)
		if err != nil {
			return err
		}
		err = row.Scan(&failover.InstanceID, &failover.UserID, &failover.Generation, &failover.RuntimeType, &failover.SourcePodID, &failover.SourcePodUID, &failover.SourceEndpoint, &failover.GatewayID, &failover.GatewayPort, &failover.WorkspacePath, &failover.SourceStopped)
		hasFailover := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if hasFailover {
			want := models.RuntimeFailoverIntent{InstanceID: instance.ID, UserID: instance.UserID, Generation: 1, RuntimeType: binding.RuntimeType, SourcePodID: pod.ID, SourcePodUID: *pod.PodUID, SourceEndpoint: *pod.AgentEndpoint, GatewayID: binding.GatewayID, GatewayPort: binding.GatewayPort, WorkspacePath: binding.WorkspacePath}
			if failover.SourceStopped || !sameRuntimeFailoverSource(failover, want) {
				return fmt.Errorf("failover intent is not the same unpublished pending reservation")
			}
		}
		if err = verify(); err != nil {
			return err
		}
		if hasFailover {
			result, e := tx.SQL().ExecContext(ctx, `DELETE FROM runtime_failover_intents WHERE instance_id=? AND user_id=? AND generation=? AND source_pod_id=? AND BINARY source_pod_uid=BINARY ? AND BINARY source_endpoint=BINARY ? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ? AND source_stopped=FALSE`, failover.InstanceID, failover.UserID, failover.Generation, failover.SourcePodID, failover.SourcePodUID, failover.SourceEndpoint, failover.GatewayID, failover.GatewayPort, failover.RuntimeType, failover.WorkspacePath)
			if e != nil {
				return e
			}
			n, e := result.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return fmt.Errorf("pending failover intent changed during rollback")
			}
		}
		result, err := tx.SQL().ExecContext(ctx, "DELETE FROM instance_runtime_bindings WHERE id=?", binding.ID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("pending reservation changed")
		}
		_, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET used_slots=(SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=?),updated_at=? WHERE id=?", pod.ID, time.Now().UTC(), pod.ID)
		released = err == nil
		return err
	}, nil)
	return released && err == nil, err
}
