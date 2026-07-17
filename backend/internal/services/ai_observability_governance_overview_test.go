package services

import (
	"context"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type stubGovernanceInstanceRepo struct {
	instances []models.Instance
}

func (s *stubGovernanceInstanceRepo) Create(*models.Instance) error { panic("not used") }
func (s *stubGovernanceInstanceRepo) GetByID(int) (*models.Instance, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) GetByAccessToken(string) (*models.Instance, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) GetByAgentBootstrapToken(string) (*models.Instance, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) GetAll(int, int) ([]models.Instance, error) { panic("not used") }
func (s *stubGovernanceInstanceRepo) CountAll() (int, error)                   { panic("not used") }
func (s *stubGovernanceInstanceRepo) GetByUserID(int, int, int) ([]models.Instance, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) CountByUserID(int) (int, error) { panic("not used") }
func (s *stubGovernanceInstanceRepo) CountActiveByMode(context.Context, string) (int, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) ExistsByUserIDAndName(int, string) (bool, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) GetAllRunning() ([]models.Instance, error) {
	return s.instances, nil
}
func (s *stubGovernanceInstanceRepo) GetV2DesiredRunning(context.Context, int) ([]models.Instance, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) GetV2Creating(context.Context, int) ([]models.Instance, error) {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) UpdateRuntimeState(context.Context, int, string, int, *string) error {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) SetWorkspacePath(context.Context, int, string) error {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) UpdateWorkspaceUsage(context.Context, int, int64) error {
	panic("not used")
}
func (s *stubGovernanceInstanceRepo) Update(*models.Instance) error { panic("not used") }
func (s *stubGovernanceInstanceRepo) Delete(int) error              { panic("not used") }

type stubGovernanceRuntimeStatusRepo struct {
	external map[int]bool
}

func (s *stubGovernanceRuntimeStatusRepo) GetByInstanceID(instanceID int) (*models.InstanceRuntimeStatus, error) {
	if s.external[instanceID] {
		raw := `{"llm_config_status":"external"}`
		return &models.InstanceRuntimeStatus{SystemInfoJSON: &raw}, nil
	}
	return nil, nil
}
func (s *stubGovernanceRuntimeStatusRepo) Create(*models.InstanceRuntimeStatus) error { panic("not used") }
func (s *stubGovernanceRuntimeStatusRepo) Update(*models.InstanceRuntimeStatus) error { panic("not used") }

func TestGetLLMGovernanceOverviewSummarizesManagedRuntimeInstances(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 10, LastSeenAt: now, FirstSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		auditRepo:       &stubSessionUsageAuditRepo{},
		instanceRepo: &stubGovernanceInstanceRepo{
			instances: []models.Instance{
				{ID: 1, UserID: 9, Name: "oc-1", Type: "openclaw", Status: "running"},
				{ID: 2, UserID: 9, Name: "oc-2", Type: "openclaw", Status: "running"},
				{ID: 3, UserID: 9, Name: "ubuntu", Type: "ubuntu", Status: "running"},
			},
		},
		runtimeStatusRepo: &stubGovernanceRuntimeStatusRepo{
			external: map[int]bool{2: true},
		},
	}

	overview, err := service.GetLLMGovernanceOverview()
	if err != nil {
		t.Fatalf("GetLLMGovernanceOverview failed: %v", err)
	}
	if overview.TotalManagedInstances != 2 {
		t.Fatalf("expected 2 managed instances, got %d", overview.TotalManagedInstances)
	}
	if overview.ExternalConfigCount != 1 {
		t.Fatalf("expected 1 external config instance, got %d", overview.ExternalConfigCount)
	}
	if len(overview.Items) != 2 {
		t.Fatalf("expected 2 overview items, got %d", len(overview.Items))
	}
}
