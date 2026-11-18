package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// RuntimeFailoverRepository is optional for legacy adapters. Production persists
// exact source identity before stop and commits replacement admission atomically.
type RuntimeFailoverRepository interface {
	GetRuntimeFailover(context.Context, int) (*models.RuntimeFailoverIntent, error)
	BeginRuntimeFailover(context.Context, models.RuntimeFailoverIntent) (*models.RuntimeFailoverIntent, error)
	ListRuntimeFailovers(context.Context, int) ([]models.RuntimeFailoverIntent, error)
	SaveRuntimeFailover(context.Context, models.RuntimeFailoverIntent) error
	AdvanceRuntimeFailover(context.Context, models.RuntimeFailoverIntent) (bool, error)
}

func (r *instanceRepository) BeginRuntimeFailover(ctx context.Context, want models.RuntimeFailoverIntent) (*models.RuntimeFailoverIntent, error) {
	var result *models.RuntimeFailoverIntent
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", want.InstanceID); err != nil {
			return err
		}
		if err := checkRuntimeUpgrade(ctx, tx, want.InstanceID); err != nil {
			return err
		}
		var prior models.RuntimeFailoverIntent
		err := tx.Collection("runtime_failover_intents").Find(db.Cond{"instance_id": want.InstanceID}).One(&prior)
		if err == nil {
			if !sameRuntimeFailoverSource(prior, want) {
				return fmt.Errorf("another source already owns runtime failover")
			}
			result = &prior
			return nil
		}
		if err != db.ErrNoMoreRows {
			return err
		}
		valid, err := runtimeFailoverCurrent(ctx, tx, want)
		if err != nil {
			return err
		}
		if !valid {
			return ErrStaleRuntimeGeneration
		}
		want.UpdatedAt = time.Now().UTC()
		if _, err := tx.Collection("runtime_failover_intents").Insert(want); err != nil {
			return err
		}
		result = &want
		return nil
	}, nil)
	return result, err
}

func sameRuntimeFailoverSource(a, b models.RuntimeFailoverIntent) bool {
	return a.InstanceID == b.InstanceID && a.UserID == b.UserID && a.Generation == b.Generation && a.RuntimeType == b.RuntimeType && a.SourcePodID == b.SourcePodID && a.SourcePodUID == b.SourcePodUID && a.SourceEndpoint == b.SourceEndpoint && a.GatewayID == b.GatewayID && a.GatewayPort == b.GatewayPort && a.WorkspacePath == b.WorkspacePath
}

func (r *instanceRepository) ListRuntimeFailovers(ctx context.Context, limit int) ([]models.RuntimeFailoverIntent, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []models.RuntimeFailoverIntent
	iter := r.sess.SQL().IteratorContext(ctx, `SELECT * FROM runtime_failover_intents WHERE attempts<=5 AND (source_stopped=TRUE OR (attempts<5 AND (next_retry_at IS NULL OR next_retry_at<=?))) ORDER BY source_stopped DESC,COALESCE(next_retry_at,'1970-01-01'),instance_id LIMIT ?`, time.Now().UTC(), limit)
	defer iter.Close()
	err := iter.All(&rows)
	return rows, err
}

func (r *instanceRepository) SaveRuntimeFailover(ctx context.Context, state models.RuntimeFailoverIntent) error {
	// Exact immutable source predicate prevents a delayed old result from changing
	// a newly requested failover. Stopped proof is monotonic across save retries.
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE runtime_failover_intents SET attempts=?,next_retry_at=?,source_stopped=(source_stopped OR ?),updated_at=? WHERE instance_id=? AND user_id=? AND generation=? AND source_pod_id=? AND BINARY source_pod_uid=BINARY ? AND BINARY source_endpoint=BINARY ? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ?`, state.Attempts, state.NextRetryAt, state.SourceStopped, time.Now().UTC(), state.InstanceID, state.UserID, state.Generation, state.SourcePodID, state.SourcePodUID, state.SourceEndpoint, state.GatewayID, state.GatewayPort, state.RuntimeType, state.WorkspacePath)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStaleRuntimeGeneration
	}
	return nil
}

func runtimeFailoverCurrent(ctx context.Context, tx db.Session, state models.RuntimeFailoverIntent) (bool, error) {
	// Journal reads may have established an older repeatable-read snapshot.
	// Locking reads keep a concurrently committed desired/idle stop authoritative.
	var blockers int
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_desired_state WHERE instance_id=? AND CAST(COALESCE(desired_power_state,'') AS BINARY)<>CAST('running' AS BINARY) FOR UPDATE`, state.InstanceID)
	if err != nil {
		return false, err
	}
	if err = row.Scan(&blockers); err != nil || blockers != 0 {
		return false, err
	}
	row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_model_activity WHERE instance_id=? AND (stop_operation_id<>'' OR (start_deadline_at IS NOT NULL AND start_deadline_at<=?)) FOR UPDATE`, state.InstanceID, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if err = row.Scan(&blockers); err != nil || blockers != 0 {
		return false, err
	}
	row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances i JOIN instance_runtime_bindings b ON b.instance_id=i.id JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND i.status IN ('running','creating','error') AND BINARY i.type=BINARY ? AND i.runtime_type='gateway' AND i.instance_mode='lite' AND BINARY i.workspace_path=BINARY ? AND b.generation=i.runtime_generation AND b.runtime_pod_id=? AND BINARY b.gateway_id=BINARY ? AND b.gateway_port=? AND BINARY b.runtime_type=BINARY ? AND BINARY b.workspace_path=BINARY ? AND BINARY p.pod_uid=BINARY ? AND BINARY p.agent_endpoint=BINARY ? FOR UPDATE`, state.InstanceID, state.UserID, state.Generation, state.RuntimeType, state.WorkspacePath, state.SourcePodID, state.GatewayID, state.GatewayPort, state.RuntimeType, state.WorkspacePath, state.SourcePodUID, state.SourceEndpoint)
	if err != nil {
		return false, err
	}
	var count int
	err = row.Scan(&count)
	return count == 1, err
}

func (r *instanceRepository) AdvanceRuntimeFailover(ctx context.Context, state models.RuntimeFailoverIntent) (bool, error) {
	advanced := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", state.InstanceID); err != nil {
			return err
		}
		if err := checkRuntimeUpgrade(ctx, tx, state.InstanceID); err != nil {
			return err
		}
		var current models.RuntimeFailoverIntent
		if err := tx.Collection("runtime_failover_intents").Find(db.Cond{"instance_id": state.InstanceID}).One(&current); err != nil {
			return err
		}
		if !sameRuntimeFailoverSource(current, state) || !current.SourceStopped {
			return nil
		}
		valid, err := runtimeFailoverCurrent(ctx, tx, state)
		if err != nil {
			return err
		}
		if !valid {
			return nil
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instances SET status='creating',runtime_generation=runtime_generation+1,runtime_error_code=NULL,runtime_error_message=?,updated_at=? WHERE id=?`, state.Reason, time.Now().UTC(), state.InstanceID); err != nil {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE instance_id=? AND runtime_pod_id=? AND generation=? AND BINARY gateway_id=BINARY ?`, state.InstanceID, state.SourcePodID, state.Generation, state.GatewayID); err != nil {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(used_slots-1,0),updated_at=? WHERE id=?`, time.Now().UTC(), state.SourcePodID); err != nil {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, `DELETE FROM runtime_failover_intents WHERE instance_id=?`, state.InstanceID); err != nil {
			return err
		}
		advanced = true
		return nil
	}, nil)
	return advanced, err
}

func (r *instanceRepository) GetRuntimeFailover(ctx context.Context, id int) (*models.RuntimeFailoverIntent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var state models.RuntimeFailoverIntent
	err := r.sess.Collection("runtime_failover_intents").Find(db.Cond{"instance_id": id}).One(&state)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// RuntimeFailoverRetirementRepository removes only an older journal after the
// service verifies native successor publication and SQL rechecks that exact owner.
type RuntimeFailoverRetirementRepository interface {
	RetireRuntimeFailover(context.Context, models.RuntimeFailoverIntent, models.RuntimeFailoverIntent) (bool, error)
}

func (r *instanceRepository) RetireRuntimeFailover(ctx context.Context, prior, successor models.RuntimeFailoverIntent) (bool, error) {
	if prior.InstanceID != successor.InstanceID || prior.UserID != successor.UserID || prior.RuntimeType != successor.RuntimeType || prior.WorkspacePath != successor.WorkspacePath || successor.Generation <= prior.Generation {
		return false, fmt.Errorf("runtime failover successor ownership does not match")
	}
	retired := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", prior.InstanceID); err != nil {
			return err
		}
		if err := checkRuntimeUpgrade(ctx, tx, prior.InstanceID); err != nil {
			return err
		}
		valid, err := runtimeFailoverCurrent(ctx, tx, successor)
		if err != nil {
			return err
		}
		if !valid {
			return nil
		}
		result, err := tx.SQL().ExecContext(ctx, `DELETE FROM runtime_failover_intents WHERE instance_id=? AND user_id=? AND generation=? AND source_pod_id=? AND BINARY source_pod_uid=BINARY ? AND BINARY source_endpoint=BINARY ? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ?`, prior.InstanceID, prior.UserID, prior.Generation, prior.SourcePodID, prior.SourcePodUID, prior.SourceEndpoint, prior.GatewayID, prior.GatewayPort, prior.RuntimeType, prior.WorkspacePath)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		retired = n == 1
		return err
	}, nil)
	return retired, err
}
