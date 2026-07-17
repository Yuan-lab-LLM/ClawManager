package services

import (
	"context"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestSkillPackageMaterializeWorkerReleasesWhenInstanceLimitReached(t *testing.T) {
	repo := &workerMaterializeJobRepoStub{
		jobs: []models.SkillPackageMaterializeJob{
			{ID: 1, InstanceID: 9, Status: MaterializeJobStatusRunning},
			{ID: 2, InstanceID: 9, Status: MaterializeJobStatusRunning},
			{ID: 3, InstanceID: 9, Status: MaterializeJobStatusRunning},
		},
	}
	service := NewSkillPackageMaterializeService(repo, nil, stubMaterializer{})
	worker := NewSkillPackageMaterializeWorker(service, time.Second, 3, 5, 2, true)

	worker.processBatch(context.Background())

	if repo.released != 1 {
		t.Fatalf("released = %d, want 1", repo.released)
	}
}

type stubMaterializer struct{}

func (stubMaterializer) materializeSkillPackageFromWorkspace(context.Context, int, string, string, int) (*models.SkillBlob, error) {
	return &models.SkillBlob{ObjectKey: "discovered/1/demo/hash.zip", ScanStatus: "completed"}, nil
}

func (stubMaterializer) syncSkillRecordFromBlob(int, *models.SkillBlob) error { return nil }

type workerMaterializeJobRepoStub struct {
	jobs     []models.SkillPackageMaterializeJob
	released int
}

func (s *workerMaterializeJobRepoStub) Create(*models.SkillPackageMaterializeJob) error { return nil }
func (s *workerMaterializeJobRepoStub) GetByID(id int) (*models.SkillPackageMaterializeJob, error) {
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			job := s.jobs[i]
			return &job, nil
		}
	}
	return nil, nil
}
func (s *workerMaterializeJobRepoStub) GetByIdempotencyKey(string) (*models.SkillPackageMaterializeJob, error) {
	return nil, nil
}
func (s *workerMaterializeJobRepoStub) ClaimNextPending(context.Context, int) ([]models.SkillPackageMaterializeJob, error) {
	return s.jobs, nil
}
func (s *workerMaterializeJobRepoStub) MarkSucceeded(int) error { return nil }
func (s *workerMaterializeJobRepoStub) MarkFailed(int, string, bool) error { return nil }
func (s *workerMaterializeJobRepoStub) MarkRunning(int) error               { return nil }
func (s *workerMaterializeJobRepoStub) ReleaseToPending(int) error {
	s.released++
	return nil
}
func (s *workerMaterializeJobRepoStub) ResetForRetry(int) error { return nil }
func (s *workerMaterializeJobRepoStub) RequeueExisting(int, int, string, string) error {
	return nil
}
func (s *workerMaterializeJobRepoStub) FindLatestBySkillID(int) (*models.SkillPackageMaterializeJob, error) {
	return nil, nil
}
func (s *workerMaterializeJobRepoStub) CountPendingByInstance(int) (int, error) { return 0, nil }
func (s *workerMaterializeJobRepoStub) ListBackfillCandidates(int) ([]repository.SkillPackageMaterializeBackfillCandidate, error) {
	return nil, nil
}
