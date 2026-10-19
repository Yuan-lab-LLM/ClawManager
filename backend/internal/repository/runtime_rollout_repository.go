package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type RuntimeRolloutRepository interface {
	Create(ctx context.Context, rollout *models.RuntimeRollout) error
	GetByID(ctx context.Context, id int64) (*models.RuntimeRollout, error)
	ListActive(ctx context.Context, runtimeType string) ([]models.RuntimeRollout, error)
	UpdateStatus(ctx context.Context, id int64, status string, startedAt *time.Time, finishedAt *time.Time, message *string) error
}

type runtimeRolloutRepository struct {
	sess db.Session
}

func NewRuntimeRolloutRepository(sess db.Session) RuntimeRolloutRepository {
	return &runtimeRolloutRepository{sess: sess}
}

func (r *runtimeRolloutRepository) ListRecent(ctx context.Context, runtimeType string) ([]models.RuntimeRollout, error) {
	cond := db.Cond{}
	if runtimeType != "" {
		cond["runtime_type"] = runtimeType
	}
	var result []models.RuntimeRollout
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := r.sess.Collection("runtime_rollouts").Find(cond).OrderBy("-id").Limit(10).All(&result)
	return result, err
}

func (r *runtimeRolloutRepository) Create(ctx context.Context, rollout *models.RuntimeRollout) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ensureTimestamps(&rollout.CreatedAt, &rollout.UpdatedAt)
	res, err := r.sess.Collection("runtime_rollouts").Insert(rollout)
	if err != nil {
		return fmt.Errorf("failed to create runtime rollout: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		rollout.ID = id
	}
	return nil
}

func (r *runtimeRolloutRepository) GetByID(ctx context.Context, id int64) (*models.RuntimeRollout, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var rollout models.RuntimeRollout
	if err := r.sess.Collection("runtime_rollouts").Find(db.Cond{"id": id}).One(&rollout); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get runtime rollout: %w", err)
	}
	return &rollout, nil
}

func (r *runtimeRolloutRepository) ListActive(ctx context.Context, runtimeType string) ([]models.RuntimeRollout, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cond := db.Cond{"status IN": []string{"pending", "running"}}
	if runtimeType != "" {
		cond["runtime_type"] = runtimeType
	}
	var rollouts []models.RuntimeRollout
	if err := r.sess.Collection("runtime_rollouts").Find(cond).OrderBy("created_at", "id").All(&rollouts); err != nil {
		return nil, fmt.Errorf("failed to list active runtime rollouts: %w", err)
	}
	return rollouts, nil
}

func (r *runtimeRolloutRepository) UpdateStatus(ctx context.Context, id int64, status string, startedAt *time.Time, finishedAt *time.Time, message *string) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_rollouts
		SET status = ?, started_at = ?, finished_at = ?, error_message = ?, updated_at = ?
		WHERE id = ?
	`, status, startedAt, finishedAt, message, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to update runtime rollout status: %w", err)
	}
	return nil
}
