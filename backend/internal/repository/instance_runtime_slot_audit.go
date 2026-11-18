package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Callers must hold the lifecycle lock and possess the normal matching confirmed
// teardown receipt. SQL state alone is never permission to release a writer.
type ConfirmedStoppedSlotBindingReleaser interface {
	DeleteConfirmedStoppedSlotBinding(context.Context, models.Instance, models.InstanceRuntimeBinding) (bool, error)
}

func (r *instanceRuntimeBindingRepository) DeleteConfirmedStoppedSlotBinding(ctx context.Context, instance models.Instance, binding models.InstanceRuntimeBinding) (bool, error) {
	if instance.Status != "stopped" || instance.ID != binding.InstanceID || instance.RuntimeGeneration != binding.Generation || instance.UserID < 1 {
		return false, fmt.Errorf("exact stopped slot owner is required")
	}
	deleted := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", instance.ID); err != nil {
			return err
		}
		if err := checkRuntimeUpgrade(ctx, tx, instance.ID); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `DELETE b FROM instance_runtime_bindings b JOIN instances i ON i.id=b.instance_id WHERE i.id=? AND i.user_id=? AND i.status='stopped' AND i.runtime_generation=? AND b.runtime_pod_id=? AND b.generation=? AND CAST(b.gateway_id AS BINARY)=CAST(? AS BINARY) AND b.gateway_port=?`, instance.ID, instance.UserID, instance.RuntimeGeneration, binding.RuntimePodID, binding.Generation, binding.GatewayID, binding.GatewayPort)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(used_slots-1,0),updated_at=? WHERE id=?`, time.Now().UTC(), binding.RuntimePodID); err != nil {
			return err
		}
		deleted = true
		return nil
	}, nil)
	return deleted, err
}
