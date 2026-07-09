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

type InstanceRuntimeBindingRepository interface {
	Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error
	GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error)
	GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error)
	ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error)
	ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error)
	UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error
	UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error
	DeleteErrorByRuntimePodIDAndGatewayPort(ctx context.Context, runtimePodID int64, gatewayPort int) (int64, error)
	DeleteByInstanceID(ctx context.Context, instanceID int) error
	DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error
	DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error)
}

type instanceRuntimeBindingRepository struct {
	sess db.Session
}

func NewInstanceRuntimeBindingRepository(sess db.Session) InstanceRuntimeBindingRepository {
	return &instanceRuntimeBindingRepository{sess: sess}
}

func (r *instanceRuntimeBindingRepository) Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
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

func (r *instanceRuntimeBindingRepository) UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
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
	res, err := r.sess.SQL().ExecContext(ctx, `
		DELETE FROM instance_runtime_bindings
		WHERE runtime_pod_id = ? AND gateway_port = ? AND state = 'error'
	`, runtimePodID, gatewayPort)
	if err != nil {
		return 0, fmt.Errorf("failed to delete stale error instance runtime binding for gateway port: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to inspect stale error instance runtime binding delete: %w", err)
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
	_, err := r.sess.SQL().ExecContext(ctx, `
		DELETE FROM instance_runtime_bindings
		WHERE instance_id = ?
	`, instanceID)
	if err != nil {
		return fmt.Errorf("failed to delete instance runtime binding: %w", err)
	}
	return nil
}

func (r *instanceRuntimeBindingRepository) DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
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
	}, nil)
}

func (r *instanceRuntimeBindingRepository) DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	deleted := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
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
	}, nil)
	return deleted, err
}
