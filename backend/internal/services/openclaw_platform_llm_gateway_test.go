package services

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

type platformLLMGatewayRepoStub struct {
	nextResourceID int
	nextSnapshotID int
	resources      map[string]*models.OpenClawConfigResource
}

func newPlatformLLMGatewayRepoStub() *platformLLMGatewayRepoStub {
	return &platformLLMGatewayRepoStub{
		nextResourceID: 1,
		nextSnapshotID: 1,
		resources:      map[string]*models.OpenClawConfigResource{},
	}
}

func (s *platformLLMGatewayRepoStub) resourceKey(userID int, resourceType, resourceKey string) string {
	return fmt.Sprintf("%d:%s:%s", userID, resourceType, resourceKey)
}

func (s *platformLLMGatewayRepoStub) ListResources(userID int, resourceType string) ([]models.OpenClawConfigResource, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) GetResourceByID(id int) (*models.OpenClawConfigResource, error) {
	for _, resource := range s.resources {
		if resource.ID == id {
			copy := *resource
			return &copy, nil
		}
	}
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) GetResourceByUserTypeKey(userID int, resourceType, resourceKey string) (*models.OpenClawConfigResource, error) {
	resource := s.resources[s.resourceKey(userID, resourceType, resourceKey)]
	if resource == nil {
		return nil, nil
	}
	copy := *resource
	return &copy, nil
}
func (s *platformLLMGatewayRepoStub) CreateResource(resource *models.OpenClawConfigResource) error {
	if resource == nil {
		return nil
	}
	resource.ID = s.nextResourceID
	s.nextResourceID++
	s.resources[s.resourceKey(resource.UserID, resource.ResourceType, resource.ResourceKey)] = resource
	return nil
}
func (s *platformLLMGatewayRepoStub) UpdateResource(resource *models.OpenClawConfigResource) error {
	return nil
}
func (s *platformLLMGatewayRepoStub) DeleteResource(id int) error { return nil }
func (s *platformLLMGatewayRepoStub) ListBundles(userID int) ([]models.OpenClawConfigBundle, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) GetBundleByID(id int) (*models.OpenClawConfigBundle, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) CreateBundle(bundle *models.OpenClawConfigBundle) error { return nil }
func (s *platformLLMGatewayRepoStub) UpdateBundle(bundle *models.OpenClawConfigBundle) error {
	return nil
}
func (s *platformLLMGatewayRepoStub) DeleteBundle(id int) error { return nil }
func (s *platformLLMGatewayRepoStub) ListBundleItems(bundleID int) ([]models.OpenClawConfigBundleItem, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) ReplaceBundleItems(bundleID int, items []models.OpenClawConfigBundleItem) error {
	return nil
}
func (s *platformLLMGatewayRepoStub) ListBundleSkills(bundleID int) ([]models.OpenClawConfigBundleSkill, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) ReplaceBundleSkills(bundleID int, items []models.OpenClawConfigBundleSkill) error {
	return nil
}
func (s *platformLLMGatewayRepoStub) CreateSnapshot(snapshot *models.OpenClawInjectionSnapshot) error {
	if snapshot == nil {
		return nil
	}
	snapshot.ID = s.nextSnapshotID
	s.nextSnapshotID++
	return nil
}
func (s *platformLLMGatewayRepoStub) UpdateSnapshot(snapshot *models.OpenClawInjectionSnapshot) error {
	return nil
}
func (s *platformLLMGatewayRepoStub) GetSnapshotByID(id int) (*models.OpenClawInjectionSnapshot, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) ListSnapshotsByUser(userID int, limit int) ([]models.OpenClawInjectionSnapshot, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) ListActiveSnapshots(userID int) ([]models.OpenClawInjectionSnapshot, error) {
	return nil, nil
}
func (s *platformLLMGatewayRepoStub) UpdateSnapshotIfUnchanged(snapshot *models.OpenClawInjectionSnapshot, expectedUpdatedAt time.Time) (bool, error) {
	return true, nil
}

func TestEnsurePlatformLLMGatewayResourceCreatesBuiltinAgentResource(t *testing.T) {
	repo := newPlatformLLMGatewayRepoStub()
	service := &openClawConfigService{repo: repo}

	resource, err := service.EnsurePlatformLLMGatewayResource(9)
	if err != nil {
		t.Fatalf("EnsurePlatformLLMGatewayResource returned error: %v", err)
	}
	if resource == nil || resource.ID <= 0 {
		t.Fatalf("expected created resource, got %+v", resource)
	}
	if resource.ResourceType != OpenClawConfigResourceTypeAgent || resource.ResourceKey != PlatformLLMGatewayResourceKey {
		t.Fatalf("unexpected resource identity: %+v", resource)
	}

	again, err := service.EnsurePlatformLLMGatewayResource(9)
	if err != nil {
		t.Fatalf("second EnsurePlatformLLMGatewayResource returned error: %v", err)
	}
	if again == nil || again.ID != resource.ID {
		t.Fatalf("expected same resource id, got %+v want %d", again, resource.ID)
	}
}

func TestCreateDefaultLLMGovernanceSnapshotCompilesPlatformGatewayAgent(t *testing.T) {
	repo := newPlatformLLMGatewayRepoStub()
	service := &openClawConfigService{repo: repo}
	instance := &models.Instance{ID: 42, UserID: 9, Type: "openclaw", Name: "oc-42"}

	snapshot, err := service.CreateDefaultLLMGovernanceSnapshot(9, instance)
	if err != nil {
		t.Fatalf("CreateDefaultLLMGovernanceSnapshot returned error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Mode != OpenClawConfigPlanModeManual {
		t.Fatalf("snapshot mode = %q, want manual", snapshot.Mode)
	}
	if !strings.Contains(snapshot.ResolvedResourcesJSON, PlatformLLMGatewayResourceKey) {
		t.Fatalf("expected platform gateway resource in snapshot, got %s", snapshot.ResolvedResourcesJSON)
	}
}
