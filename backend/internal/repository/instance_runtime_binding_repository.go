package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type InstanceRuntimeBindingRepository interface {
	Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error
	GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error)
	GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error)
	ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error)
	ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error)
	UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error
	UpdateGatewayAssignment(ctx context.Context, instanceID int, generation int, gatewayID string, pid *int, state string, lastHealthAt *time.Time) error
	UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error
	DeleteErrorByRuntimePodIDAndGatewayPort(ctx context.Context, runtimePodID int64, gatewayPort int) (int64, error)
	DeleteByInstanceID(ctx context.Context, instanceID int) error
	DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error
	DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error)
}

// ReportedGatewayBindingReconciler repairs one narrowly defined control-plane
// drift from an authoritative Runtime snapshot. Keeping this optional avoids
// imposing 8.1 reconciliation rules on older Runtime repository consumers.
type ReportedGatewayBindingReconciler interface {
	ReconcileReportedGateway(ctx context.Context, binding *models.InstanceRuntimeBinding, expectedInstanceGeneration, portBlockSize int) (bool, error)
}

// PendingGatewayBindingReleaser conditionally releases an abandoned start
// reservation after a fresh Runtime snapshot confirms that it does not exist.
type PendingGatewayBindingReleaser interface {
	DeletePendingByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int, updatedBefore time.Time) (bool, error)
}

// FailedGatewayBindingReleaser conditionally releases an old Agent-reported
// process failure after that Agent has confirmed the writer is gone. The
// generation/state predicate prevents a delayed recovery pass from deleting a
// newer binding.
type FailedGatewayBindingReleaser interface {
	DeleteFailedByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error)
}

type instanceRuntimeBindingRepository struct {
	sess db.Session
}

func NewInstanceRuntimeBindingRepository(sess db.Session) InstanceRuntimeBindingRepository {
	return &instanceRuntimeBindingRepository{sess: sess}
}

func (r *instanceRuntimeBindingRepository) Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, binding.InstanceID, func(local *instanceRepository) error {
		return (&instanceRuntimeBindingRepository{local.sess}).create(ctx, binding)
	})
}

func (r *instanceRuntimeBindingRepository) create(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ensureTimestamps(&binding.CreatedAt, &binding.UpdatedAt)
	res, err := r.sess.Collection("instance_runtime_bindings").Insert(binding)
	if err != nil {
		return fmt.Errorf("failed to create instance runtime binding: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		binding.ID = id
	}
	return nil
}

func (r *instanceRuntimeBindingRepository) ReconcileReportedGateway(ctx context.Context, binding *models.InstanceRuntimeBinding, expectedInstanceGeneration, portBlockSize int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if binding == nil || binding.InstanceID <= 0 || binding.RuntimePodID <= 0 || binding.Generation <= 0 || (expectedInstanceGeneration != binding.Generation && expectedInstanceGeneration != binding.Generation+1) || portBlockSize <= 0 {
		return false, nil
	}
	reconciled := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		var status string
		var generation int
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT status, runtime_generation FROM instances WHERE id = ? FOR UPDATE`, binding.InstanceID)
		if err != nil {
			return err
		}
		if err := row.Scan(&status, &generation); err != nil {
			return err
		}
		if (status != "error" && status != "creating") || generation != expectedInstanceGeneration {
			return nil
		}
		var existingID, existingPodID sql.NullInt64
		var existingGatewayID, existingState sql.NullString
		var existingGeneration sql.NullInt64
		row, err = tx.SQL().QueryRowContext(ctx, `
			SELECT MAX(id), MAX(runtime_pod_id), MAX(gateway_id), MAX(state), MAX(generation)
			FROM instance_runtime_bindings WHERE instance_id = ?
			FOR UPDATE
		`, binding.InstanceID)
		if err != nil {
			return err
		}
		if err := row.Scan(&existingID, &existingPodID, &existingGatewayID, &existingState, &existingGeneration); err != nil {
			return err
		}
		if existingID.Valid {
			expectedPendingID := fmt.Sprintf("pending-%d-%d", binding.InstanceID, existingGeneration.Int64)
			if !existingPodID.Valid || existingPodID.Int64 != binding.RuntimePodID || !existingGeneration.Valid || int(existingGeneration.Int64) != expectedInstanceGeneration || !strings.EqualFold(existingState.String, "creating") || existingGatewayID.String != expectedPendingID {
				return nil
			}
			if _, err := tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE id = ?`, existingID.Int64); err != nil {
				return err
			}
		}
		var portConflicts int
		row, err = tx.SQL().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM instance_runtime_bindings
			WHERE runtime_pod_id = ?
			  AND gateway_port <= ?
			  AND gateway_port + ? - 1 >= ?
		`, binding.RuntimePodID, binding.GatewayPort+portBlockSize-1, portBlockSize, binding.GatewayPort)
		if err != nil {
			return err
		}
		if err := row.Scan(&portConflicts); err != nil {
			return err
		}
		if portConflicts != 0 {
			return nil
		}
		now := time.Now().UTC()
		if _, err := tx.SQL().ExecContext(ctx, `
			INSERT INTO instance_runtime_bindings (
				instance_id, runtime_pod_id, runtime_type, gateway_id, gateway_port,
				gateway_pid, workspace_path, state, generation, last_health_at,
				error_message, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'running', ?, ?, NULL, ?, ?)
		`, binding.InstanceID, binding.RuntimePodID, binding.RuntimeType, binding.GatewayID, binding.GatewayPort,
			binding.GatewayPID, binding.WorkspacePath, binding.Generation, binding.LastHealthAt, now, now); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `
			UPDATE instances
			SET status = 'running', runtime_generation = ?, runtime_error_message = NULL, updated_at = ?
			WHERE id = ? AND status IN ('error', 'creating') AND runtime_generation = ?
		`, binding.Generation, now, binding.InstanceID, expectedInstanceGeneration)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("instance %d changed while reconciling reported gateway", binding.InstanceID)
		}
		reconciled = true
		return nil
	}, nil)
	if err != nil {
		return false, fmt.Errorf("reconcile reported gateway binding: %w", err)
	}
	return reconciled, nil
}

func (r *instanceRuntimeBindingRepository) GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var binding models.InstanceRuntimeBinding
	if err := r.sess.Collection("instance_runtime_bindings").Find(db.Cond{"instance_id": instanceID}).One(&binding); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance runtime binding: %w", err)
	}
	return &binding, nil
}

func (r *instanceRuntimeBindingRepository) GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var binding models.InstanceRuntimeBinding
	if err := r.sess.Collection("instance_runtime_bindings").Find(db.Cond{"instance_id": instanceID, "state": "running"}).One(&binding); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get running instance runtime binding: %w", err)
	}
	return &binding, nil
}

func (r *instanceRuntimeBindingRepository) ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var bindings []models.InstanceRuntimeBinding
	if err := r.sess.Collection("instance_runtime_bindings").Find(db.Cond{"runtime_pod_id": runtimePodID}).OrderBy("id").All(&bindings); err != nil {
		return nil, fmt.Errorf("failed to list instance runtime bindings: %w", err)
	}
	return bindings, nil
}

func (r *instanceRuntimeBindingRepository) ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(runtimePodIDs) == 0 {
		return []models.InstanceRuntimeBinding{}, nil
	}
	var bindings []models.InstanceRuntimeBinding
	if err := r.sess.Collection("instance_runtime_bindings").Find(db.Cond{"runtime_pod_id IN": runtimePodIDs}).OrderBy("runtime_pod_id", "id").All(&bindings); err != nil {
		return nil, fmt.Errorf("failed to list instance runtime bindings by pods: %w", err)
	}
	return bindings, nil
}

// UpdateGatewayAssignment fills a binding that was reserved by the control
// plane before the runtime agent was asked to start the gateway. The port is
// intentionally not updated here: it is the reservation being confirmed.
func (r *instanceRuntimeBindingRepository) UpdateGatewayAssignment(ctx context.Context, instanceID int, generation int, gatewayID string, pid *int, state string, lastHealthAt *time.Time) error {
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, instanceID, func(local *instanceRepository) error {
		return (&instanceRuntimeBindingRepository{local.sess}).updateGatewayAssignment(ctx, instanceID, generation, gatewayID, pid, state, lastHealthAt)
	})
}

func (r *instanceRuntimeBindingRepository) updateGatewayAssignment(ctx context.Context, instanceID int, generation int, gatewayID string, pid *int, state string, lastHealthAt *time.Time) error {
	res, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instance_runtime_bindings
		SET gateway_id = ?, gateway_pid = ?, state = ?, last_health_at = ?, error_message = NULL, updated_at = ?
		WHERE instance_id = ? AND generation <= ?
	`, gatewayID, pid, state, lastHealthAt, time.Now().UTC(), instanceID, generation)
	if err != nil {
		return fmt.Errorf("failed to update gateway assignment: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect gateway assignment update: %w", err)
	}
	if affected == 0 {
		currentGeneration, err := r.getGeneration(ctx, instanceID)
		if err != nil {
			return err
		}
		if currentGeneration > generation {
			return ErrStaleRuntimeGeneration
		}
	}
	return nil
}
func (r *instanceRuntimeBindingRepository) UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, instanceID, func(local *instanceRepository) error {
		return (&instanceRuntimeBindingRepository{local.sess}).updateRunning(ctx, instanceID, generation, gatewayID, port, pid)
	})
}

func (r *instanceRuntimeBindingRepository) updateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
	now := time.Now().UTC()
	res, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instance_runtime_bindings
		SET state = 'running', generation = ?, gateway_id = ?, gateway_port = ?, gateway_pid = ?,
			last_health_at = ?, error_message = NULL, updated_at = ?
		WHERE instance_id = ? AND generation <= ?
	`, generation, gatewayID, port, pid, now, now, instanceID, generation)
	if err != nil {
		return fmt.Errorf("failed to update running instance runtime binding: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect running instance runtime binding update: %w", err)
	}
	if affected == 0 {
		currentGeneration, err := r.getGeneration(ctx, instanceID)
		if err != nil {
			return err
		}
		if currentGeneration > generation {
			return ErrStaleRuntimeGeneration
		}
	}
	return nil
}

func (r *instanceRuntimeBindingRepository) UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error {
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, instanceID, func(local *instanceRepository) error {
		return (&instanceRuntimeBindingRepository{local.sess}).updateState(ctx, instanceID, generation, state, message)
	})
}

func (r *instanceRuntimeBindingRepository) updateState(ctx context.Context, instanceID int, generation int, state string, message *string) error {
	res, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instance_runtime_bindings
		SET state = ?, generation = ?, error_message = ?, updated_at = ?
		WHERE instance_id = ? AND generation <= ?
	`, state, generation, message, time.Now().UTC(), instanceID, generation)
	if err != nil {
		return fmt.Errorf("failed to update instance runtime binding state: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect instance runtime binding state update: %w", err)
	}
	if affected == 0 {
		currentGeneration, err := r.getGeneration(ctx, instanceID)
		if err != nil {
			return err
		}
		if currentGeneration > generation {
			return ErrStaleRuntimeGeneration
		}
	}
	return nil
}

func (r *instanceRuntimeBindingRepository) DeleteErrorByRuntimePodIDAndGatewayPort(ctx context.Context, runtimePodID int64, gatewayPort int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var candidates []models.InstanceRuntimeBinding
	if err := r.sess.WithContext(ctx).Collection("instance_runtime_bindings").Find(db.Cond{"runtime_pod_id": runtimePodID, "gateway_port": gatewayPort, "state": "error"}).All(&candidates); err != nil {
		return 0, err
	}
	var affected int64
	for _, binding := range candidates {
		err := (&instanceRepository{r.sess}).withUnfencedInstance(ctx, binding.InstanceID, func(local *instanceRepository) error {
			res, err := local.sess.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE id=? AND generation=? AND runtime_pod_id=? AND gateway_port=? AND state='error'`, binding.ID, binding.Generation, runtimePodID, gatewayPort)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			affected += n
			return err
		})
		if errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
			continue
		}
		if err != nil {
			return affected, err
		}
	}
	return affected, nil
}
func (r *instanceRuntimeBindingRepository) getGeneration(ctx context.Context, instanceID int) (int, error) {
	var currentGeneration int
	row, err := r.sess.SQL().QueryRowContext(ctx, `
		SELECT generation
		FROM instance_runtime_bindings
		WHERE instance_id = ?
	`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to query instance runtime binding generation: %w", err)
	}
	if err := row.Scan(&currentGeneration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrStaleRuntimeGeneration
		}
		return 0, fmt.Errorf("failed to scan instance runtime binding generation: %w", err)
	}
	return currentGeneration, nil
}

func (r *instanceRuntimeBindingRepository) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, instanceID, func(local *instanceRepository) error {
		_, err := local.sess.SQL().ExecContext(ctx, `
		DELETE FROM instance_runtime_bindings
		WHERE instance_id = ?
	`, instanceID)
		if err != nil {
			return fmt.Errorf("failed to delete instance runtime binding: %w", err)
		}
		return nil
	})
}

func (r *instanceRuntimeBindingRepository) DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, instanceID, func(local *instanceRepository) error {
		tx := local.sess
		res, err := tx.SQL().ExecContext(ctx, `
			DELETE FROM instance_runtime_bindings
			WHERE instance_id = ? AND runtime_pod_id = ?
		`, instanceID, runtimePodID)
		if err != nil {
			return fmt.Errorf("failed to delete instance runtime binding: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to inspect instance runtime binding delete: %w", err)
		}
		if affected == 0 {
			return nil
		}
		if _, err := tx.SQL().ExecContext(ctx, `
			UPDATE runtime_pods
			SET used_slots = CASE WHEN used_slots > 0 THEN used_slots - 1 ELSE 0 END, updated_at = ?
			WHERE id = ?
		`, time.Now().UTC(), runtimePodID); err != nil {
			return fmt.Errorf("failed to release runtime pod slot: %w", err)
		}
		return nil
	})
}

func (r *instanceRuntimeBindingRepository) DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deleted := false
	err := (&instanceRepository{r.sess}).withUnfencedInstance(ctx, instanceID, func(local *instanceRepository) error {
		tx := local.sess
		res, err := tx.SQL().ExecContext(ctx, `
			DELETE FROM instance_runtime_bindings
			WHERE instance_id = ? AND runtime_pod_id = ? AND generation = ? AND state = 'running'
		`, instanceID, runtimePodID, generation)
		if err != nil {
			return fmt.Errorf("failed to delete current running instance runtime binding: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to inspect current running instance runtime binding delete: %w", err)
		}
		if affected == 0 {
			return nil
		}
		if _, err := tx.SQL().ExecContext(ctx, `
			UPDATE runtime_pods
			SET used_slots = CASE WHEN used_slots > 0 THEN used_slots - 1 ELSE 0 END, updated_at = ?
			WHERE id = ?
		`, time.Now().UTC(), runtimePodID); err != nil {
			return fmt.Errorf("failed to release runtime pod slot: %w", err)
		}
		deleted = true
		return nil
	})
	return deleted, err
}

func (r *instanceRuntimeBindingRepository) DeleteFailedByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deleted := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		res, err := tx.SQL().ExecContext(ctx, `
			DELETE FROM instance_runtime_bindings
			WHERE instance_id = ? AND runtime_pod_id = ? AND generation = ?
			  AND state IN ('error', 'failed', 'stopped')
		`, instanceID, runtimePodID, generation)
		if err != nil {
			return fmt.Errorf("failed to delete confirmed failed instance runtime binding: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to inspect confirmed failed binding delete: %w", err)
		}
		if affected == 0 {
			return nil
		}
		if _, err := tx.SQL().ExecContext(ctx, `
			UPDATE runtime_pods
			SET used_slots = CASE WHEN used_slots > 0 THEN used_slots - 1 ELSE 0 END, updated_at = ?
			WHERE id = ?
		`, time.Now().UTC(), runtimePodID); err != nil {
			return fmt.Errorf("failed to release confirmed failed runtime slot: %w", err)
		}
		deleted = true
		return nil
	}, nil)
	return deleted, err
}

func (r *instanceRuntimeBindingRepository) DeletePendingByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int, updatedBefore time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deleted := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		expectedGatewayID := fmt.Sprintf("pending-%d-%d", instanceID, generation)
		res, err := tx.SQL().ExecContext(ctx, `
			DELETE FROM instance_runtime_bindings
			WHERE instance_id = ? AND runtime_pod_id = ? AND generation = ?
			  AND state = 'creating' AND gateway_id = ? AND updated_at <= ?
		`, instanceID, runtimePodID, generation, expectedGatewayID, updatedBefore)
		if err != nil {
			return fmt.Errorf("failed to delete abandoned pending runtime binding: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to inspect pending runtime binding delete: %w", err)
		}
		if affected == 0 {
			return nil
		}
		if _, err := tx.SQL().ExecContext(ctx, `
			UPDATE runtime_pods
			SET used_slots = CASE WHEN used_slots > 0 THEN used_slots - 1 ELSE 0 END, updated_at = ?
			WHERE id = ?
		`, time.Now().UTC(), runtimePodID); err != nil {
			return fmt.Errorf("failed to release abandoned pending runtime slot: %w", err)
		}
		deleted = true
		return nil
	}, nil)
	return deleted, err
}
