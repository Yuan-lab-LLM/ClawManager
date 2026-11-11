package repository

import (
	"context"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type RuntimeMaintenanceRepository interface {
	Get(context.Context, int) (*models.RuntimeMaintenance, error)
	Poll(context.Context, int, int, models.RuntimeMaintenanceReport) (*models.RuntimeMaintenance, error)
}
type runtimeMaintenanceRepository struct{ sess db.Session }

func NewRuntimeMaintenanceRepository(sess db.Session) RuntimeMaintenanceRepository {
	return &runtimeMaintenanceRepository{sess}
}
func (r *runtimeMaintenanceRepository) Get(ctx context.Context, id int) (*models.RuntimeMaintenance, error) {
	var result models.RuntimeMaintenance
	err := r.sess.WithContext(ctx).Collection("gateway_runtime_maintenance").Find(db.Cond{"instance_id": id}).One(&result)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fence, err := NewRuntimeUpgradeRepository(r.sess).Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if fence != nil && fence.Active() {
		if fence.Operation != result.Operation || fence.OwnerID != result.OwnerID || fence.Generation != result.Generation || result.Cancelled {
			return nil, models.ErrRuntimeUpgradeConflict
		}
		result.UpgradeFenced = true
	}
	return &result, nil
}
func (r *runtimeMaintenanceRepository) Poll(ctx context.Context, id, owner int, report models.RuntimeMaintenanceReport) (*models.RuntimeMaintenance, error) {
	if !report.Valid() {
		return nil, models.ErrRuntimeMaintenanceConflict
	}
	// Never accept body-supplied instance/owner; also fence against a concurrent restart.
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances i JOIN users u ON u.id=i.user_id WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND u.is_active=TRUE`, id, owner, report.Generation)
	if err != nil {
		return nil, err
	}
	var count int
	if err = row.Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, models.ErrModelUnavailable
	}
	if report.Operation != "" {
		_, err = r.sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_maintenance SET gate_closed=?,active_executions=?,active_admissions=?,active_jobs=?,queued_inputs=?,reported_at=?
 WHERE instance_id=? AND owner_user_id=? AND generation=? AND operation_id=? AND cancelled=FALSE AND (deadline>? OR EXISTS
 (SELECT 1 FROM gateway_runtime_upgrades u WHERE u.instance_id=gateway_runtime_maintenance.instance_id AND u.operation_id=gateway_runtime_maintenance.operation_id AND u.state NOT IN ('cancelled','completed')))`,
			report.GateClosed, report.Active, report.Admissions, report.Jobs, report.Queued, time.Now().UTC(), id, owner, report.Generation, report.Operation, time.Now().UTC())
		if err != nil {
			return nil, err
		}
	}
	state, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if state != nil && (state.OwnerID != owner || state.Generation != report.Generation) {
		return nil, nil
	}
	return state, nil
}
