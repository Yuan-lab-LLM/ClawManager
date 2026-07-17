package services

import (
	"context"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const (
	MaterializeJobStatusPending   = "pending"
	MaterializeJobStatusRunning     = "running"
	MaterializeJobStatusSucceeded   = "succeeded"
	MaterializeJobStatusFailed      = "failed"
	MaterializeJobStatusCancelled   = "cancelled"
	MaterializeTriggerSync          = "sync"
	MaterializeTriggerRetry         = "retry"
	MaterializeTriggerImport        = "import"
	MaterializeTriggerPublish       = "publish"
	MaterializeTriggerBackfill      = "backfill"
)

type EnqueueMaterializeRequest struct {
	InstanceID     int
	SkillID        int
	BlobID         int
	WorkspaceDir   string
	ContentHash    string
	TriggerSource  string
	IdempotencyKey string
}

type skillBlobReader interface {
	GetBlobByID(id int) (*models.SkillBlob, error)
}

type SkillPackageMaterializeService struct {
	jobRepo  repository.SkillPackageMaterializeJobRepository
	blobRepo skillBlobReader
	worker   skillPackageMaterializer
}

type skillPackageMaterializer interface {
	materializeSkillPackageFromWorkspace(ctx context.Context, instanceID int, workspaceDir, contentHash string, targetBlobID int) (*models.SkillBlob, error)
	syncSkillRecordFromBlob(skillID int, blob *models.SkillBlob) error
}

func SkillServiceAsMaterializer(service SkillService) skillPackageMaterializer {
	if impl, ok := service.(*skillService); ok {
		return impl
	}
	return nil
}

func NewSkillPackageMaterializeService(jobRepo repository.SkillPackageMaterializeJobRepository, blobRepo skillBlobReader, worker skillPackageMaterializer) *SkillPackageMaterializeService {
	return &SkillPackageMaterializeService{jobRepo: jobRepo, blobRepo: blobRepo, worker: worker}
}

func (m *SkillPackageMaterializeService) Enqueue(ctx context.Context, req EnqueueMaterializeRequest) (*models.SkillPackageMaterializeJob, error) {
	if m == nil || m.jobRepo == nil {
		return nil, fmt.Errorf("skill package materialize service is not configured")
	}
	workspaceDir := sanitizeWorkspaceRelativePath(strings.TrimSpace(req.WorkspaceDir))
	contentHash := strings.TrimSpace(req.ContentHash)
	if req.InstanceID <= 0 || req.SkillID <= 0 || req.BlobID <= 0 || workspaceDir == "" || contentHash == "" {
		return nil, fmt.Errorf("invalid materialize enqueue request")
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("materialize-%d-%s", req.InstanceID, contentHash)
	}
	if existing, err := m.jobRepo.GetByIdempotencyKey(idempotencyKey); err != nil {
		return nil, err
	} else if existing != nil {
		if strings.EqualFold(strings.TrimSpace(existing.Status), MaterializeJobStatusSucceeded) {
			return existing, nil
		}
		if strings.EqualFold(strings.TrimSpace(existing.Status), MaterializeJobStatusFailed) {
			if err := m.jobRepo.RequeueExisting(existing.ID, req.BlobID, contentHash, workspaceDir); err != nil {
				return nil, err
			}
			return m.jobRepo.GetByID(existing.ID)
		}
		if strings.EqualFold(strings.TrimSpace(existing.Status), MaterializeJobStatusPending) ||
			strings.EqualFold(strings.TrimSpace(existing.Status), MaterializeJobStatusRunning) {
			return existing, nil
		}
	}
	if m.blobRepo != nil {
		blob, err := m.blobRepo.GetBlobByID(req.BlobID)
		if err != nil {
			return nil, err
		}
		if blob != nil && strings.TrimSpace(blob.ObjectKey) != "" {
			if existing, err := m.jobRepo.GetByIdempotencyKey(idempotencyKey); err != nil {
				return nil, err
			} else if existing != nil {
				if !strings.EqualFold(strings.TrimSpace(existing.Status), MaterializeJobStatusSucceeded) {
					if err := m.jobRepo.MarkSucceeded(existing.ID); err != nil {
						return nil, err
					}
					existing, err = m.jobRepo.GetByID(existing.ID)
					if err != nil {
						return nil, err
					}
				}
				return existing, nil
			}
			job := &models.SkillPackageMaterializeJob{
				InstanceID:     req.InstanceID,
				SkillID:        req.SkillID,
				BlobID:         req.BlobID,
				WorkspaceDir:   workspaceDir,
				ContentHash:    contentHash,
				Status:         MaterializeJobStatusSucceeded,
				MaxAttempts:    5,
				IdempotencyKey: idempotencyKey,
				TriggerSource:  strings.TrimSpace(req.TriggerSource),
			}
			if job.TriggerSource == "" {
				job.TriggerSource = MaterializeTriggerSync
			}
			if err := m.jobRepo.Create(job); err != nil {
				return nil, err
			}
			if !strings.EqualFold(strings.TrimSpace(job.Status), MaterializeJobStatusSucceeded) {
				if err := m.jobRepo.MarkSucceeded(job.ID); err != nil {
					return nil, err
				}
				job, err = m.jobRepo.GetByID(job.ID)
				if err != nil {
					return nil, err
				}
			}
			return job, nil
		}
	}
	trigger := strings.TrimSpace(req.TriggerSource)
	if trigger == "" {
		trigger = MaterializeTriggerSync
	}

	job := &models.SkillPackageMaterializeJob{
		InstanceID:     req.InstanceID,
		SkillID:        req.SkillID,
		BlobID:         req.BlobID,
		WorkspaceDir:   workspaceDir,
		ContentHash:    contentHash,
		Status:         MaterializeJobStatusPending,
		MaxAttempts:    5,
		IdempotencyKey: idempotencyKey,
		TriggerSource:  trigger,
	}
	if err := m.jobRepo.Create(job); err != nil {
		return nil, err
	}
	return job, nil
}

func (m *SkillPackageMaterializeService) ReleaseToPending(id int) error {
	if m == nil || m.jobRepo == nil {
		return fmt.Errorf("skill package materialize service is not configured")
	}
	return m.jobRepo.ReleaseToPending(id)
}

func (m *SkillPackageMaterializeService) ClaimNextPending(ctx context.Context, limit int) ([]models.SkillPackageMaterializeJob, error) {
	if m == nil || m.jobRepo == nil {
		return nil, fmt.Errorf("skill package materialize service is not configured")
	}
	return m.jobRepo.ClaimNextPending(ctx, limit)
}

func (m *SkillPackageMaterializeService) ProcessJob(ctx context.Context, jobID int) error {
	if m == nil || m.jobRepo == nil || m.worker == nil {
		return fmt.Errorf("skill package materialize service is not configured")
	}
	job, err := m.jobRepo.GetByID(jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("materialize job not found")
	}
	if strings.EqualFold(strings.TrimSpace(job.Status), MaterializeJobStatusPending) {
		if err := m.jobRepo.MarkRunning(jobID); err != nil {
			return err
		}
		job, err = m.jobRepo.GetByID(jobID)
		if err != nil {
			return err
		}
		if job == nil {
			return fmt.Errorf("materialize job not found")
		}
	}

	blob, err := m.worker.materializeSkillPackageFromWorkspace(ctx, job.InstanceID, job.WorkspaceDir, job.ContentHash, job.BlobID)
	if err != nil {
		retryable := !strings.Contains(strings.ToLower(err.Error()), "md5 mismatch")
		if markErr := m.jobRepo.MarkFailed(job.ID, err.Error(), retryable); markErr != nil {
			return markErr
		}
		return err
	}
	if err := m.worker.syncSkillRecordFromBlob(job.SkillID, blob); err != nil {
		if markErr := m.jobRepo.MarkFailed(job.ID, err.Error(), true); markErr != nil {
			return markErr
		}
		return err
	}
	return m.jobRepo.MarkSucceeded(job.ID)
}

func (m *SkillPackageMaterializeService) RetryJob(skillID int) error {
	if m == nil || m.jobRepo == nil {
		return fmt.Errorf("skill package materialize service is not configured")
	}
	return m.jobRepo.ResetForRetry(skillID)
}

func (m *SkillPackageMaterializeService) BackfillOnce(ctx context.Context, limit int) (int, error) {
	if m == nil || m.jobRepo == nil {
		return 0, fmt.Errorf("skill package materialize service is not configured")
	}
	candidates, err := m.jobRepo.ListBackfillCandidates(limit)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, candidate := range candidates {
		_, err := m.Enqueue(ctx, EnqueueMaterializeRequest{
			InstanceID:     candidate.InstanceID,
			SkillID:        candidate.SkillID,
			BlobID:         candidate.BlobID,
			WorkspaceDir:   candidate.WorkspaceDir,
			ContentHash:    candidate.ContentHash,
			TriggerSource:  MaterializeTriggerBackfill,
			IdempotencyKey: fmt.Sprintf("materialize-%d-%s", candidate.InstanceID, candidate.ContentHash),
		})
		if err != nil {
			return enqueued, err
		}
		enqueued++
	}
	return enqueued, nil
}

func (m *SkillPackageMaterializeService) FindLatestBySkillID(skillID int) (*models.SkillPackageMaterializeJob, error) {
	if m == nil || m.jobRepo == nil {
		return nil, nil
	}
	return m.jobRepo.FindLatestBySkillID(skillID)
}

func (m *SkillPackageMaterializeService) GetObservedStatus(skillID int, blob *models.SkillBlob) (*string, *string) {
	if m == nil || m.jobRepo == nil {
		return nil, nil
	}
	if blob != nil && strings.TrimSpace(blob.ObjectKey) != "" {
		return nil, nil
	}
	job, err := m.jobRepo.FindLatestBySkillID(skillID)
	if err != nil || job == nil {
		return nil, nil
	}
	status := strings.TrimSpace(job.Status)
	if status == "" {
		return nil, nil
	}
	var errSummary *string
	if job.LastError != nil && strings.TrimSpace(*job.LastError) != "" {
		summary := truncateCollectError(*job.LastError, 512)
		if summary != "" {
			errSummary = &summary
		}
	}
	return &status, errSummary
}

func materializeBlockedReason(job *models.SkillPackageMaterializeJob) *string {
	if job == nil {
		return nil
	}
	reason := func(value string) *string { return &value }
	switch strings.TrimSpace(job.Status) {
	case MaterializeJobStatusRunning:
		return reason("skill_package_materializing")
	case MaterializeJobStatusPending:
		return reason("skill_package_materializing")
	case MaterializeJobStatusFailed:
		return reason("skill_package_materialize_failed")
	default:
		return nil
	}
}
