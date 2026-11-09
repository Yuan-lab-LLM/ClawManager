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
	// Locking reads observe a stop committed while this transaction waited for
	// the instance lock, rather than an earlier repeatable-read snapshot.
	row, err := r.sess.SQL().QueryRowContext(ctx, "SELECT runtime_generation,status FROM instances WHERE id=? FOR UPDATE", expected.ID)
	if err != nil {
		return false, err
	}
	var generation int
	var status string
	if err = row.Scan(&generation, &status); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	row, err = r.sess.SQL().QueryRowContext(ctx, "SELECT desired_power_state FROM instance_desired_state WHERE instance_id=? FOR UPDATE", expected.ID)
	if err != nil {
		return false, err
	}
	var desired string
	err = row.Scan(&desired)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return generation == expected.RuntimeGeneration && status == expected.Status && (errors.Is(err, sql.ErrNoRows) || desired == "running"), nil
}

func (r *instanceRepository) SaveRuntimeRecovery(ctx context.Context, expected models.Instance, state models.InstanceRuntimeRecovery) (bool, error) {
	if state.InstanceID != expected.ID || state.Attempts < 0 || state.Attempts > 3 || (state.Phase != "" && state.Phase != "source_stopped" && state.Phase != "placing") {
		return false, fmt.Errorf("invalid runtime recovery state")
	}
	saved := false
	err := r.withUnfencedInstance(ctx, expected.ID, func(tx *instanceRepository) error {
		matches, err := tx.runtimeRecoveryIntentMatches(ctx, expected)
		if err != nil || !matches {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, `INSERT INTO instance_runtime_recovery (instance_id,attempts,next_retry_at,reason,healthy_since,source_pod_id,source_gateway_port,generation,updated_at,phase) VALUES (?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE attempts=VALUES(attempts),next_retry_at=VALUES(next_retry_at),reason=VALUES(reason),healthy_since=VALUES(healthy_since),source_pod_id=VALUES(source_pod_id),source_gateway_port=VALUES(source_gateway_port),generation=VALUES(generation),updated_at=VALUES(updated_at),phase=VALUES(phase)`, state.InstanceID, state.Attempts, state.NextRetryAt, state.Reason, state.HealthySince, state.SourcePodID, state.SourceGatewayPort, state.Generation, state.UpdatedAt, state.Phase)
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
		result, err := tx.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='creating',runtime_generation=runtime_generation+1,runtime_error_code=NULL,runtime_error_message=NULL,updated_at=? WHERE id=? AND runtime_generation=? AND status=? AND NOT EXISTS (SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id=instances.id) AND EXISTS (SELECT 1 FROM instance_runtime_recovery r WHERE r.instance_id=instances.id AND r.generation=instances.runtime_generation AND r.phase='source_stopped')`, time.Now().UTC(), expected.ID, expected.RuntimeGeneration, expected.Status)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, "UPDATE instance_runtime_recovery SET phase='placing',generation=?,updated_at=? WHERE instance_id=? AND generation=? AND phase='source_stopped'", expected.RuntimeGeneration+1, time.Now().UTC(), expected.ID, expected.RuntimeGeneration)
		advanced = err == nil
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

// ResumeRuntimeRecoveryPlacement only re-arms an already stopped source's exact
// replacement generation. Explicit administrator retry does not advance again.
func (r *instanceRepository) ResumeRuntimeRecoveryPlacement(ctx context.Context, expected models.Instance) (bool, error) {
	resumed := false
	err := r.withUnfencedInstance(ctx, expected.ID, func(tx *instanceRepository) error {
		matches, err := tx.runtimeRecoveryIntentMatches(ctx, expected)
		if err != nil || !matches {
			return err
		}
		if err = checkAutoStopRuntimeState(ctx, tx.sess, expected.ID, "creating", expected.RuntimeGeneration); err != nil {
			return err
		}
		result, err := tx.sess.SQL().ExecContext(ctx, "UPDATE instances SET status='creating',runtime_error_code=NULL,runtime_error_message=NULL,updated_at=? WHERE id=? AND runtime_generation=? AND status=? AND NOT EXISTS (SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id=instances.id) AND EXISTS (SELECT 1 FROM instance_runtime_recovery r WHERE r.instance_id=instances.id AND r.generation=instances.runtime_generation AND r.phase='placing')", time.Now().UTC(), expected.ID, expected.RuntimeGeneration, expected.Status)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		resumed = n == 1
		return err
	})
	return resumed, err
}
