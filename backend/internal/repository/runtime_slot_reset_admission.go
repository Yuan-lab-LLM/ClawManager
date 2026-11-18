package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

var ErrRuntimeSlotResetFenced = errors.New("Runtime gateway port is fenced by a slot reset")

// A reset reserves the physical port until release. Its terminal tombstone
// covers the retired owner. If the owner was unknown, a report may only
// update an existing verified reservation; it cannot restore an unbound writer.
func slotResetBlocksBinding(ctx context.Context, tx db.Session, b *models.InstanceRuntimeBinding, unreservedReport bool) (bool, error) {
	canonical := fmt.Sprintf("gw-%d-%d", b.InstanceID, b.Generation)
	pending := fmt.Sprintf("pending-%d-%d", b.InstanceID, b.Generation)
	alias := b.GatewayID
	switch b.GatewayID {
	case canonical:
		alias = pending
	case pending:
		alias = canonical
	}
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM runtime_slot_reset_requests r JOIN runtime_pods p
		  ON p.id=r.source_pod_id AND BINARY p.pod_uid=BINARY r.pod_uid
		WHERE r.source_pod_id=? AND r.gateway_port=? AND (
		  r.active=1 OR (? AND (r.instance_id=0 OR r.source_generation=0 OR r.source_gateway_id=''))
		  OR (r.instance_id=? AND r.source_generation=? AND r.source_generation>0
		  AND r.source_gateway_id<>'' AND r.source_gateway_id IN (?,?))))`,
		b.RuntimePodID, b.GatewayPort, unreservedReport, b.InstanceID, b.Generation, b.GatewayID, alias)
	if err != nil {
		return false, err
	}
	var blocked bool
	err = row.Scan(&blocked)
	return blocked, err
}

// ApplyReportedGateway compares the observed placement again under the instance
// row lock, then commits binding health and instance state together.
func (r *instanceRuntimeBindingRepository) ApplyReportedGateway(ctx context.Context, expected, report models.InstanceRuntimeBinding, instanceState string) error {
	if expected.ID <= 0 || report.InstanceID != expected.InstanceID || report.Generation != expected.Generation || report.RuntimePodID != expected.RuntimePodID || report.GatewayID != expected.GatewayID || report.GatewayPort != expected.GatewayPort || (report.WorkspacePath != "" && report.WorkspacePath != expected.WorkspacePath) {
		return nil
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", report.InstanceID); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		if blocked, err := slotResetBlocksBinding(ctx, tx, &report, false); err != nil || blocked {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM instance_runtime_bindings WHERE id=? AND instance_id=? AND runtime_pod_id=? AND generation=? AND gateway_port=? AND BINARY gateway_id=BINARY ? AND BINARY workspace_path=BINARY ? FOR UPDATE`, expected.ID, expected.InstanceID, expected.RuntimePodID, expected.Generation, expected.GatewayPort, expected.GatewayID, expected.WorkspacePath)
		if err != nil {
			return err
		}
		var id int64
		if err = row.Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		if err = checkRuntimeUpgrade(ctx, tx, report.InstanceID); err != nil {
			return err
		}
		bindings := instanceRuntimeBindingRepository{tx}
		if report.State == "running" {
			err = bindings.updateRunning(ctx, report.InstanceID, report.Generation, report.GatewayID, report.GatewayPort, report.GatewayPID)
		} else {
			err = bindings.updateState(ctx, report.InstanceID, report.Generation, report.State, report.ErrorMessage)
		}
		if err != nil {
			return err
		}
		return (&instanceRepository{tx}).updateReportedRuntimeStateForBindingLocked(ctx, report.InstanceID, instanceState, report.Generation, report.ErrorMessage, report.RuntimePodID, report.GatewayID, report.GatewayPort, report.WorkspacePath)
	}, nil)
}
