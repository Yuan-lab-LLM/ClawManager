package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// ConfirmedRecoveryBindingReleaser fences a teardown acknowledgement to the
// exact source assignment; a changed same-generation gateway is never freed.
type ConfirmedRecoveryBindingReleaser interface {
	DeleteConfirmedRecoveryBinding(context.Context, models.InstanceRuntimeBinding) (bool, error)
}

func (r *instanceRuntimeBindingRepository) DeleteConfirmedRecoveryBinding(ctx context.Context, expected models.InstanceRuntimeBinding) (bool, error) {
	deleted := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", expected.InstanceID); err != nil {
			return err
		}
		if err := checkRuntimeUpgrade(ctx, tx, expected.InstanceID); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE instance_id=? AND runtime_pod_id=? AND generation=? AND CAST(gateway_id AS BINARY)=CAST(? AS BINARY) AND gateway_port=? AND state IN ('error','failed','stopped','unhealthy')`, expected.InstanceID, expected.RuntimePodID, expected.Generation, expected.GatewayID, expected.GatewayPort)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(used_slots-1,0),updated_at=? WHERE id=?`, time.Now().UTC(), expected.RuntimePodID)
		if err != nil {
			return fmt.Errorf("release confirmed recovery source slot: %w", err)
		}
		// The receipt and release commit together. A crash after this transaction
		// must resume generation advance even when the retry budget is exhausted.
		result, err = tx.SQL().ExecContext(ctx, "UPDATE instance_runtime_recovery SET phase='source_stopped',updated_at=? WHERE instance_id=? AND generation=? AND attempts>0", time.Now().UTC(), expected.InstanceID, expected.Generation)
		if err != nil {
			return err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("confirmed source release requires its durable recovery intent")
		}
		deleted = true
		return nil
	}, nil)
	return deleted, err
}
