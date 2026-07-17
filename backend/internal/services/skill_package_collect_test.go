package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
)

type stubCommandRepo struct {
	failed *models.InstanceCommand
}

func (s *stubCommandRepo) Create(*models.InstanceCommand) error { panic("not used") }
func (s *stubCommandRepo) Update(*models.InstanceCommand) error { panic("not used") }
func (s *stubCommandRepo) GetByID(int) (*models.InstanceCommand, error) { panic("not used") }
func (s *stubCommandRepo) GetByInstanceIdempotencyKey(int, string) (*models.InstanceCommand, error) {
	panic("not used")
}
func (s *stubCommandRepo) GetNextPendingByInstance(int) (*models.InstanceCommand, error) {
	panic("not used")
}
func (s *stubCommandRepo) ListByInstanceID(int, int) ([]models.InstanceCommand, error) {
	panic("not used")
}
func (s *stubCommandRepo) FindLatestFailedCollectSkillPackage(string) (*models.InstanceCommand, error) {
	return s.failed, nil
}

func TestPublishBlockedReasonCollectFailed(t *testing.T) {
	errMsg := `unexpected status 500: {"error":"skill package md5 mismatch: expected abc got def","success":false}`
	svc := &skillService{
		commandRepo: &stubCommandRepo{
			failed: &models.InstanceCommand{
				CommandType:  "collect_skill_package",
				Status:       "failed",
				ErrorMessage: &errMsg,
				FinishedAt:   ptrTime(time.Now()),
			},
		},
	}
	skill := &models.Skill{ID: 2, Status: skillStatusActive, SourceType: skillSourceDiscovered}
	blob := &models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""}

	reason := svc.publishBlockedReasonForSkill(skill, blob, nil, false, false)
	if reason == nil || *reason != "skill_package_collect_failed" {
		t.Fatalf("expected skill_package_collect_failed, got %v", reason)
	}
	collectErr := svc.resolvePackageCollectError(skill.ID, blob, nil, false)
	if collectErr == nil || *collectErr != errMsg {
		t.Fatalf("expected package collect error summary, got %v", collectErr)
	}
}

func TestPublishBlockedReasonScanFailed(t *testing.T) {
	svc := &skillService{}
	skill := &models.Skill{ID: 1, Status: skillStatusActive, SourceType: skillSourceUploaded}
	blob := &models.SkillBlob{ScanStatus: "failed", RiskLevel: skillRiskUnknown, ObjectKey: "discovered/1/demo.zip"}

	reason := svc.publishBlockedReasonForSkill(skill, blob, nil, false, false)
	if reason == nil || *reason != "skill_scan_failed" {
		t.Fatalf("expected skill_scan_failed, got %v", reason)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
