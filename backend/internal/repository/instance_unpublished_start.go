package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
)

// ResumeUnpublishedRuntimeStart preserves a never-published reservation after an
// explicit Start. The caller must hold the lifecycle lock and freshly prove the
// predecessor stopped and this generation's business Secret absent. This SQL
// admission grants no publication or stop authority and never changes intent.
func (r *instanceRepository) ResumeUnpublishedRuntimeStart(ctx context.Context, expected models.Instance, pending models.InstanceRuntimeBinding) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if expected.ID <= 0 || expected.UserID <= 0 || expected.RuntimeGeneration <= 0 ||
		(expected.Status != "stopped" && expected.Status != "error") || expected.InstanceMode != "lite" || expected.RuntimeType != "gateway" ||
		expected.WorkspacePath == nil || *expected.WorkspacePath == "" || pending.ID <= 0 || pending.RuntimePodID <= 0 ||
		pending.InstanceID != expected.ID || pending.RuntimeType == "" || pending.RuntimeType != expected.Type ||
		pending.Generation != expected.RuntimeGeneration || pending.State != "creating" || pending.GatewayPID != nil ||
		pending.GatewayID != fmt.Sprintf("pending-%d-%d", expected.ID, expected.RuntimeGeneration) ||
		pending.GatewayPort <= 0 || pending.GatewayPort > 65535 || pending.WorkspacePath != *expected.WorkspacePath {
		return false, nil
	}
	resumed := false
	err := r.withUnfencedInstance(ctx, expected.ID, func(tx *instanceRepository) error {
		// Paths and protocol identities must match byte-for-byte even when the
		// database's default collation ignores case or trailing spaces.
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=? AND user_id=? AND BINARY type=BINARY ? AND BINARY instance_mode=BINARY ? AND BINARY runtime_type=BINARY ? AND runtime_generation=? AND BINARY status=BINARY ? AND BINARY workspace_path=BINARY ? FOR UPDATE`, expected.ID, expected.UserID, expected.Type, expected.InstanceMode, expected.RuntimeType, expected.RuntimeGeneration, expected.Status, *expected.WorkspacePath)
		if err != nil {
			return err
		}
		var id int
		if err = row.Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		// The upgrade check may have opened a repeatable-read snapshot. These
		// locking reads must observe any newer command Stop already committed.
		row, err = tx.sess.SQL().QueryRowContext(ctx, `SELECT desired_power_state FROM instance_desired_state WHERE instance_id=? FOR UPDATE`, expected.ID)
		if err != nil {
			return err
		}
		var power string
		if err = row.Scan(&power); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		if power != "running" {
			return nil
		}
		row, err = tx.sess.SQL().QueryRowContext(ctx, `SELECT generation,stop_operation_id,COALESCE(start_deadline_at>UTC_TIMESTAMP(6),FALSE) FROM instance_model_activity WHERE instance_id=? FOR UPDATE`, expected.ID)
		if err != nil {
			return err
		}
		var generation int
		var stop string
		var unexpired bool
		if err = row.Scan(&generation, &stop, &unexpired); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		if generation != expected.RuntimeGeneration || stop != "" || !unexpired {
			return nil
		}
		row, err = tx.sess.SQL().QueryRowContext(ctx, `SELECT id FROM instance_runtime_bindings WHERE id=? AND instance_id=? AND runtime_pod_id=? AND BINARY runtime_type=BINARY ? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY workspace_path=BINARY ? AND BINARY state=BINARY ? AND generation=? AND gateway_pid IS NULL FOR UPDATE`, pending.ID, pending.InstanceID, pending.RuntimePodID, pending.RuntimeType, pending.GatewayID, pending.GatewayPort, pending.WorkspacePath, pending.State, pending.Generation)
		if err != nil {
			return err
		}
		var bindingID int64
		if err = row.Scan(&bindingID); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		result, err := tx.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='creating',runtime_error_code=NULL,runtime_error_message=NULL,updated_at=? WHERE id=? AND status=? AND runtime_generation=? AND EXISTS (SELECT 1 FROM instance_model_activity a WHERE a.instance_id=instances.id AND a.generation=? AND a.stop_operation_id='' AND a.start_deadline_at>UTC_TIMESTAMP(6))`, time.Now().UTC(), expected.ID, expected.Status, expected.RuntimeGeneration, expected.RuntimeGeneration)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		resumed = n == 1
		return err
	})
	return resumed && err == nil, err
}
