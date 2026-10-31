package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type RuntimeMaintenanceRepository interface {
	Get(context.Context, int) (*models.RuntimeMaintenance, error)
	Begin(context.Context, int, int, int, int, time.Duration) (*models.RuntimeMaintenance, error)
	Cancel(context.Context, int, string) error
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
func (r *runtimeMaintenanceRepository) Begin(ctx context.Context, id, owner, generation, actor int, timeout time.Duration) (*models.RuntimeMaintenance, error) {
	if id <= 0 || owner <= 0 || actor <= 0 || generation < 0 || timeout < 10*time.Second || timeout > 10*time.Minute {
		return nil, models.ErrRuntimeMaintenanceConflict
	}
	var result *models.RuntimeMaintenance
	err := r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT i.runtime_generation FROM instances i JOIN users u ON u.id=i.user_id
 WHERE i.id=? AND i.user_id=? AND u.is_active=TRUE FOR UPDATE`, id, owner)
		if err != nil {
			return err
		}
		var current int
		if err = row.Scan(&current); err != nil || current != generation {
			return models.ErrRuntimeMaintenanceConflict
		}
		if err := checkRuntimeUpgrade(ctx, sess, id); err != nil {
			return err
		}
		protocol, err := NewRuntimeProtocolRepository(sess).Get(ctx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if protocol == nil || protocol.OwnerUserID != owner || protocol.Generation != generation || protocol.Protocol != 1 ||
			protocol.ReportedAt.After(now) || now.Sub(protocol.ReportedAt) >= 30*time.Second {
			return models.ErrRuntimeMaintenanceConflict
		}
		var caps []string
		if json.Unmarshal([]byte(protocol.CapabilitiesJSON), &caps) != nil {
			return models.ErrRuntimeMaintenanceConflict
		}
		supported := false
		for _, cap := range caps {
			if cap == "maintenance_drain" {
				supported = true
			}
		}
		if !supported {
			return models.ErrRuntimeMaintenanceConflict
		}
		local := &runtimeMaintenanceRepository{sess}
		previous, err := local.Get(ctx, id)
		if err != nil {
			return err
		}
		if previous != nil && previous.Draining(now) {
			return models.ErrRuntimeMaintenanceConflict
		}
		var token [16]byte
		if _, err = rand.Read(token[:]); err != nil {
			return err
		}
		result = &models.RuntimeMaintenance{InstanceID: id, OwnerID: owner, Generation: generation, Operation: hex.EncodeToString(token[:]), RequestedBy: actor, Deadline: now.Add(timeout)}
		_, err = sess.SQL().ExecContext(ctx, `INSERT INTO gateway_runtime_maintenance(instance_id,owner_user_id,generation,operation_id,requested_by,deadline)
 VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE owner_user_id=VALUES(owner_user_id),generation=VALUES(generation),operation_id=VALUES(operation_id),
 requested_by=VALUES(requested_by),deadline=VALUES(deadline),cancelled=FALSE,gate_closed=FALSE,active_executions=0,active_admissions=0,active_jobs=0,queued_inputs=0,reported_at=NULL`,
			id, owner, generation, result.Operation, actor, result.Deadline)
		return err
	}, nil)
	return result, err
}
func (r *runtimeMaintenanceRepository) Cancel(ctx context.Context, id int, operation string) error {
	return r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=? FOR UPDATE`, id)
		if err != nil {
			return err
		}
		var locked int
		if err := row.Scan(&locked); err != nil {
			return err
		}
		if err := checkRuntimeUpgrade(ctx, sess, id); err != nil {
			return err
		}
		return (&runtimeMaintenanceRepository{sess}).cancelUnfenced(ctx, id, operation)
	}, nil)
}

func (r *runtimeMaintenanceRepository) cancelUnfenced(ctx context.Context, id int, operation string) error {
	if len(operation) != 32 {
		return models.ErrRuntimeMaintenanceConflict
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_maintenance SET cancelled=TRUE WHERE instance_id=? AND operation_id=?`, id, operation)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		state, err := r.Get(ctx, id)
		if err != nil {
			return err
		}
		if state == nil || state.Operation != operation {
			return models.ErrRuntimeMaintenanceConflict
		}
	}
	return nil
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
