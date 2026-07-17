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

type SkillPackageMaterializeBackfillCandidate struct {
	InstanceID   int
	SkillID      int
	BlobID       int
	WorkspaceDir string
	ContentHash  string
}

type SkillPackageMaterializeJobRepository interface {
	Create(job *models.SkillPackageMaterializeJob) error
	GetByID(id int) (*models.SkillPackageMaterializeJob, error)
	GetByIdempotencyKey(key string) (*models.SkillPackageMaterializeJob, error)
	ClaimNextPending(ctx context.Context, limit int) ([]models.SkillPackageMaterializeJob, error)
	MarkSucceeded(id int) error
	MarkFailed(id int, errMsg string, retryable bool) error
	MarkRunning(id int) error
	ReleaseToPending(id int) error
	ResetForRetry(skillID int) error
	RequeueExisting(id, blobID int, contentHash, workspaceDir string) error
	FindLatestBySkillID(skillID int) (*models.SkillPackageMaterializeJob, error)
	CountPendingByInstance(instanceID int) (int, error)
	ListBackfillCandidates(limit int) ([]SkillPackageMaterializeBackfillCandidate, error)
}

type skillPackageMaterializeJobRepository struct {
	sess db.Session
}

func NewSkillPackageMaterializeJobRepository(sess db.Session) SkillPackageMaterializeJobRepository {
	return &skillPackageMaterializeJobRepository{sess: sess}
}

func (r *skillPackageMaterializeJobRepository) Create(job *models.SkillPackageMaterializeJob) error {
	existing, err := r.GetByIdempotencyKey(job.IdempotencyKey)
	if err != nil {
		return err
	}
	if existing != nil {
		*job = *existing
		return nil
	}
	if strings.TrimSpace(job.Status) == "" {
		job.Status = "pending"
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 5
	}
	if strings.TrimSpace(job.TriggerSource) == "" {
		job.TriggerSource = "sync"
	}
	ensureTimestamps(&job.CreatedAt, &job.UpdatedAt)
	res, err := r.sess.Collection("skill_package_materialize_jobs").Insert(job)
	if err != nil {
		if isDuplicateEntryError(err) {
			existing, findErr := r.GetByIdempotencyKey(job.IdempotencyKey)
			if findErr != nil {
				return findErr
			}
			if existing != nil {
				*job = *existing
				return nil
			}
		}
		return fmt.Errorf("failed to create skill package materialize job: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		job.ID = int(id)
	}
	return nil
}

func (r *skillPackageMaterializeJobRepository) GetByID(id int) (*models.SkillPackageMaterializeJob, error) {
	var item models.SkillPackageMaterializeJob
	if err := r.sess.Collection("skill_package_materialize_jobs").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill package materialize job: %w", err)
	}
	return &item, nil
}

func (r *skillPackageMaterializeJobRepository) GetByIdempotencyKey(key string) (*models.SkillPackageMaterializeJob, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	var item models.SkillPackageMaterializeJob
	if err := r.sess.Collection("skill_package_materialize_jobs").Find(db.Cond{"idempotency_key": key}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill package materialize job by idempotency key: %w", err)
	}
	return &item, nil
}

func (r *skillPackageMaterializeJobRepository) ClaimNextPending(ctx context.Context, limit int) ([]models.SkillPackageMaterializeJob, error) {
	if limit <= 0 {
		limit = 1
	}
	var ids []int
	iter := r.sess.SQL().IteratorContext(ctx, `
		SELECT id FROM skill_package_materialize_jobs
		WHERE status = 'pending'
		ORDER BY created_at ASC, id ASC
		LIMIT ?`, limit)
	for iter.Next() {
		var id int
		if err := iter.Scan(&id); err != nil {
			iter.Close()
			return nil, fmt.Errorf("failed to scan pending materialize job id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := iter.Err(); err != nil {
		iter.Close()
		return nil, fmt.Errorf("failed to list pending materialize jobs: %w", err)
	}
	iter.Close()

	now := time.Now().UTC()
	claimed := make([]models.SkillPackageMaterializeJob, 0, len(ids))
	for _, id := range ids {
		res, err := r.sess.SQL().ExecContext(ctx, `
			UPDATE skill_package_materialize_jobs
			SET status = 'running',
			    attempt_count = attempt_count + 1,
			    started_at = ?,
			    updated_at = ?
			WHERE id = ? AND status = 'pending'`, now, now, id)
		if err != nil {
			return nil, fmt.Errorf("failed to claim materialize job %d: %w", id, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("failed to read claim rows affected for job %d: %w", id, err)
		}
		if affected == 0 {
			continue
		}
		item, err := r.GetByID(id)
		if err != nil {
			return nil, err
		}
		if item != nil {
			claimed = append(claimed, *item)
		}
	}
	return claimed, nil
}

func (r *skillPackageMaterializeJobRepository) MarkSucceeded(id int) error {
	now := time.Now().UTC()
	_, err := r.sess.SQL().Exec(`
		UPDATE skill_package_materialize_jobs
		SET status = 'succeeded',
		    finished_at = ?,
		    last_error = NULL,
		    updated_at = ?
		WHERE id = ?`, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark materialize job succeeded: %w", err)
	}
	return nil
}

func (r *skillPackageMaterializeJobRepository) MarkRunning(id int) error {
	now := time.Now().UTC()
	_, err := r.sess.SQL().Exec(`
		UPDATE skill_package_materialize_jobs
		SET status = 'running',
		    attempt_count = attempt_count + 1,
		    started_at = ?,
		    updated_at = ?
		WHERE id = ? AND status = 'pending'`, now, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark materialize job running: %w", err)
	}
	return nil
}

func (r *skillPackageMaterializeJobRepository) MarkFailed(id int, errMsg string, retryable bool) error {
	job, err := r.GetByID(id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("materialize job not found")
	}
	now := time.Now().UTC()
	status := "failed"
	if retryable && job.AttemptCount < job.MaxAttempts {
		status = "pending"
	}
	trimmed := strings.TrimSpace(errMsg)
	var lastError *string
	if trimmed != "" {
		lastError = &trimmed
	}
	update := map[string]interface{}{
		"status":     status,
		"last_error": lastError,
		"updated_at": now,
	}
	if status == "failed" {
		update["finished_at"] = now
	}
	if err := r.sess.Collection("skill_package_materialize_jobs").Find(db.Cond{"id": id}).Update(update); err != nil {
		return fmt.Errorf("failed to mark materialize job failed: %w", err)
	}
	return nil
}

func (r *skillPackageMaterializeJobRepository) ReleaseToPending(id int) error {
	now := time.Now().UTC()
	_, err := r.sess.SQL().Exec(`
		UPDATE skill_package_materialize_jobs
		SET status = 'pending',
		    started_at = NULL,
		    updated_at = ?
		WHERE id = ? AND status = 'running'`, now, id)
	if err != nil {
		return fmt.Errorf("failed to release materialize job to pending: %w", err)
	}
	return nil
}

func (r *skillPackageMaterializeJobRepository) ResetForRetry(skillID int) error {
	job, err := r.FindLatestBySkillID(skillID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("materialize job not found")
	}
	return r.RequeueExisting(job.ID, job.BlobID, job.ContentHash, job.WorkspaceDir)
}

func (r *skillPackageMaterializeJobRepository) RequeueExisting(id, blobID int, contentHash, workspaceDir string) error {
	now := time.Now().UTC()
	_, err := r.sess.SQL().Exec(`
		UPDATE skill_package_materialize_jobs
		SET status = 'pending',
		    last_error = NULL,
		    finished_at = NULL,
		    started_at = NULL,
		    blob_id = ?,
		    content_hash = ?,
		    workspace_dir = ?,
		    updated_at = ?
		WHERE id = ?`, blobID, strings.TrimSpace(contentHash), sanitizeMaterializeWorkspaceDir(workspaceDir), now, id)
	if err != nil {
		return fmt.Errorf("failed to requeue materialize job: %w", err)
	}
	return nil
}

func sanitizeMaterializeWorkspaceDir(value string) string {
	return strings.TrimSpace(value)
}

func (r *skillPackageMaterializeJobRepository) FindLatestBySkillID(skillID int) (*models.SkillPackageMaterializeJob, error) {
	var item models.SkillPackageMaterializeJob
	if err := r.sess.Collection("skill_package_materialize_jobs").Find(db.Cond{"skill_id": skillID}).OrderBy("-created_at", "-id").One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find latest materialize job: %w", err)
	}
	return &item, nil
}

func (r *skillPackageMaterializeJobRepository) CountPendingByInstance(instanceID int) (int, error) {
	row, err := r.sess.SQL().QueryRow(`
		SELECT COUNT(*) FROM skill_package_materialize_jobs
		WHERE instance_id = ? AND status IN ('pending', 'running')`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to count pending materialize jobs: %w", err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to scan pending materialize job count: %w", err)
	}
	return count, nil
}

func (r *skillPackageMaterializeJobRepository) ListBackfillCandidates(limit int) ([]SkillPackageMaterializeBackfillCandidate, error) {
	if limit <= 0 {
		limit = 500
	}
	iter := r.sess.SQL().Iterator(`
		SELECT isk.instance_id, isk.skill_id, sv.blob_id, isk.workspace_dir, sb.content_hash
		FROM instance_skills isk
		JOIN instances i ON i.id = isk.instance_id
		JOIN skills s ON s.id = isk.skill_id
		JOIN skill_versions sv ON sv.id = s.current_version_id
		JOIN skill_blobs sb ON sb.id = sv.blob_id
		WHERE isk.status = 'active'
		  AND (LOWER(TRIM(i.instance_mode)) = 'lite' OR LOWER(TRIM(i.runtime_type)) = 'gateway')
		  AND TRIM(sb.object_key) = ''
		  AND isk.workspace_dir IS NOT NULL
		  AND TRIM(isk.workspace_dir) <> ''
		ORDER BY isk.updated_at ASC
		LIMIT ?`, limit)
	defer iter.Close()

	result := make([]SkillPackageMaterializeBackfillCandidate, 0)
	for iter.Next() {
		var item SkillPackageMaterializeBackfillCandidate
		if err := iter.Scan(&item.InstanceID, &item.SkillID, &item.BlobID, &item.WorkspaceDir, &item.ContentHash); err != nil {
			return nil, fmt.Errorf("failed to scan backfill candidate: %w", err)
		}
		result = append(result, item)
	}
	if err := iter.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to list backfill candidates: %w", err)
	}
	return result, nil
}
