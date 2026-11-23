package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// RetireUnpublishedUpgradeReservation retires only an SQL reservation owned by
// the current upgrade claim. The verifier must establish either an unpublished
// successor and finalized predecessor, or a real native teardown of the exact
// published successor. It runs inside the SAME admission fence as publication.
// This SQL method never acknowledges a process stop, deletes credentials or
// changes the instance generation.
func (r *instanceRuntimeBindingRepository) RetireUnpublishedUpgradeReservation(ctx context.Context, b models.InstanceRuntimeBinding, claim string, verify func(context.Context, int, string) error) (bool, error) {
	if claim == "" || verify == nil || b.ID <= 0 || b.RuntimeType != "openclaw" || b.Generation <= 1 || b.State != "creating" || b.GatewayPID != nil || b.GatewayID != fmt.Sprintf("pending-%d-%d", b.InstanceID, b.Generation) {
		return false, fmt.Errorf("exact claimed unpublished upgrade reservation required")
	}
	retired := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var user, generation int
		var workspace string
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,workspace_path FROM instances WHERE id=? AND BINARY type=BINARY 'openclaw' AND BINARY instance_mode=BINARY 'lite' FOR UPDATE`, b.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&user, &generation, &workspace); err != nil {
			return err
		}
		if generation != b.Generation || workspace != b.WorkspacePath {
			return fmt.Errorf("upgrade reservation instance identity changed")
		}
		var id int64
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT ui.id FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=? AND ui.source_generation=? AND BINARY ui.operation_token=BINARY ? AND ui.operation_deadline_at>UTC_TIMESTAMP(6) AND r.status='running' AND ui.operation_phase='session_migration' FOR UPDATE`, b.InstanceID, b.Generation, claim)
		if err != nil {
			return err
		}
		if err = row.Scan(&id); err != nil {
			return err
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT id FROM instance_runtime_bindings WHERE id=? AND instance_id=? AND runtime_pod_id=? AND generation=? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY workspace_path=BINARY ? AND BINARY runtime_type=BINARY 'openclaw' AND BINARY state=BINARY 'creating' AND gateway_pid IS NULL FOR UPDATE`, b.ID, b.InstanceID, b.RuntimePodID, b.Generation, b.GatewayID, b.GatewayPort, b.WorkspacePath)
		if err != nil {
			return err
		}
		if err = row.Scan(&id); err != nil {
			return err
		}
		if err = verify(ctx, user, workspace); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return fmt.Errorf("upgrade reservation changed during retirement")
		}
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(used_slots-1,(SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=?)),updated_at=? WHERE id=?`, b.RuntimePodID, time.Now().UTC(), b.RuntimePodID); err != nil {
			return err
		}
		retired = true
		return nil
	}, nil)
	return retired && err == nil, err
}
