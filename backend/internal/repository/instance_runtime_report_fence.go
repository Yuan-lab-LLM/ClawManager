package repository

import (
	"context"
	"database/sql"
)

// UpdateReportedRuntimeStateForBinding keeps observation separate from business intent.
// Recovery stops the old writer before advancing SQL generation; its own stopped
// report must not turn the retained run intent into a manual stop.
func (r *instanceRepository) UpdateReportedRuntimeStateForBinding(ctx context.Context, id int, status string, generation int, message *string, podID int64, gatewayID string, port int, workspace string) error {
	return r.withInstanceRowLock(ctx, id, func(tx *instanceRepository) error {
		var held int
		// Keep this a consistent read: recovery commits lock operation -> instance.
		// Taking an operation lock here would reverse that order. The instance lock
		// prevents source release/generation advancement during the report decision.
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_lifecycle_manual_guards g JOIN runtime_management_operations o ON o.id=g.token COLLATE utf8mb4_unicode_ci WHERE g.instance_id=? AND o.kind='eviction' AND o.status IN ('pending','processing','needs_attention') AND JSON_UNQUOTE(JSON_EXTRACT(o.request_json,'$.mode'))='recovery'`, id)
		if err != nil {
			return err
		}
		if err = row.Scan(&held); err != nil {
			return err
		}
		if held > 0 {
			return nil
		}
		var latestStatus string
		var latestGeneration int
		row, err = tx.sess.SQL().QueryRowContext(ctx, "SELECT status,runtime_generation FROM instances WHERE id=? FOR UPDATE", id)
		if err != nil {
			return err
		}
		if err = row.Scan(&latestStatus, &latestGeneration); err == sql.ErrNoRows {
			return nil
		} else if err != nil {
			return err
		}
		if latestStatus == "deleting" || latestStatus == "deleted" || latestGeneration != generation {
			return nil
		}
		var bindingPod int64
		var bindingGeneration, bindingPort int
		var bindingGateway, bindingWorkspace string
		row, err = tx.sess.SQL().QueryRowContext(ctx, "SELECT runtime_pod_id,generation,gateway_id,gateway_port,workspace_path FROM instance_runtime_bindings WHERE instance_id=? FOR UPDATE", id)
		if err != nil {
			return err
		}
		if err = row.Scan(&bindingPod, &bindingGeneration, &bindingGateway, &bindingPort, &bindingWorkspace); err == sql.ErrNoRows {
			return nil
		} else if err != nil {
			return err
		}
		if bindingPod != podID || bindingGeneration != generation || bindingGateway != gatewayID || bindingPort != port || bindingWorkspace != workspace {
			return nil
		}
		if err = checkRuntimeUpgrade(ctx, tx.sess, id); err != nil {
			return err
		}
		return tx.updateRuntimeStateWithCodeLocked(ctx, id, status, generation, message, nil)
	})
}
