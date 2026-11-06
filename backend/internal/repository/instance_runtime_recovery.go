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

// InstanceRuntimeRecoveryRepository mutations run under the instance lifecycle
// lock at the caller. Row/generation/desired-state predicates also fence stale
// reporters and user stop intents that have already been committed.
type InstanceRuntimeRecoveryRepository interface {
	GetRuntimeRecovery(context.Context, int) (*models.InstanceRuntimeRecovery, error)
	SaveRuntimeRecovery(context.Context, models.Instance, models.InstanceRuntimeRecovery) (bool, error)
	AdvanceRuntimeRecovery(context.Context, models.Instance) (bool, error)
	ResetRuntimeRecovery(context.Context, int) error
}

func (r *instanceRepository) GetRuntimeRecovery(ctx context.Context, id int) (*models.InstanceRuntimeRecovery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var state models.InstanceRuntimeRecovery
	err := r.sess.Collection("instance_runtime_recovery").Find(db.Cond{"instance_id": id}).One(&state)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read runtime recovery: %w", err)
	}
	return &state, nil
}

func (r *instanceRepository) runtimeRecoveryIntentMatches(ctx context.Context, expected models.Instance) (bool, error) {
	if expected.Status != "running" && expected.Status != "error" && expected.Status != "creating" {
		return false, nil
	}
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT runtime_generation,status,NOT EXISTS (SELECT 1 FROM instance_desired_state d WHERE d.instance_id=instances.id AND d.desired_power_state<>'running') FROM instances WHERE id=?`, expected.ID)
	if err != nil {
		return false, err
	}
	var generation int
	var status string
	var desired bool
	err = row.Scan(&generation, &status, &desired)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return generation == expected.RuntimeGeneration && status == expected.Status && desired, err
}

func (r *instanceRepository) SaveRuntimeRecovery(ctx context.Context, expected models.Instance, state models.InstanceRuntimeRecovery) (bool, error) {
	if state.InstanceID != expected.ID || state.Attempts < 0 || state.Attempts > 3 {
		return false, fmt.Errorf("invalid runtime recovery state")
	}
	saved := false
	err := r.withUnfencedInstance(ctx, expected.ID, func(tx *instanceRepository) error {
		matches, err := tx.runtimeRecoveryIntentMatches(ctx, expected)
		if err != nil || !matches {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, `INSERT INTO instance_runtime_recovery (instance_id,attempts,next_retry_at,reason,healthy_since,source_pod_id,source_gateway_port,generation,updated_at) VALUES (?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE attempts=VALUES(attempts),next_retry_at=VALUES(next_retry_at),reason=VALUES(reason),healthy_since=VALUES(healthy_since),source_pod_id=VALUES(source_pod_id),source_gateway_port=VALUES(source_gateway_port),generation=VALUES(generation),updated_at=VALUES(updated_at)`, state.InstanceID, state.Attempts, state.NextRetryAt, state.Reason, state.HealthySince, state.SourcePodID, state.SourceGatewayPort, state.Generation, state.UpdatedAt)
		saved = err == nil
		return err
	})
	return saved, err
}

func (r *instanceRepository) AdvanceRuntimeRecovery(ctx context.Context, expected models.Instance) (bool, error) {
	advanced := false
	err := r.withUnfencedInstance(ctx, expected.ID, func(tx *instanceRepository) error {
		matches, err := tx.runtimeRecoveryIntentMatches(ctx, expected)
		if err != nil || !matches {
			return err
		}
		if err = checkAutoStopRuntimeState(ctx, tx.sess, expected.ID, "creating", expected.RuntimeGeneration+1); err != nil {
			return err
		}
		result, err := tx.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='creating',runtime_generation=runtime_generation+1,runtime_error_code=NULL,runtime_error_message=NULL,updated_at=? WHERE id=? AND runtime_generation=? AND status=? AND NOT EXISTS (SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id=instances.id)`, time.Now().UTC(), expected.ID, expected.RuntimeGeneration, expected.Status)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		advanced = n == 1
		return err
	})
	return advanced, err
}

func (r *instanceRepository) ResetRuntimeRecovery(ctx context.Context, id int) error {
	return r.withUnfencedInstance(ctx, id, func(tx *instanceRepository) error {
		_, err := tx.sess.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_recovery WHERE instance_id=?`, id)
		return err
	})
}
