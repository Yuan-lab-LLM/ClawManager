package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/go-sql-driver/mysql"
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
func (s *platformLLMGatewayRepoStub) CreateBundle(bundle *models.OpenClawConfigBundle) error {
	return nil
}
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

// Force two service replicas to observe the missing row before either inserts.
type concurrentPlatformGatewayRepo struct {
	*platformLLMGatewayRepoStub
	mu    sync.Mutex
	reads int
	ready chan struct{}
}

func (r *concurrentPlatformGatewayRepo) GetResourceByUserTypeKey(userID int, kind, key string) (*models.OpenClawConfigResource, error) {
	r.mu.Lock()
	item, err := r.platformLLMGatewayRepoStub.GetResourceByUserTypeKey(userID, kind, key)
	r.reads++
	initial := r.reads <= 4
	if r.reads == 4 {
		close(r.ready)
	}
	r.mu.Unlock()
	if initial {
		<-r.ready
	}
	return item, err
}

func (r *concurrentPlatformGatewayRepo) CreateResource(item *models.OpenClawConfigResource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.resourceKey(item.UserID, item.ResourceType, item.ResourceKey)
	if r.resources[key] != nil {
		return fmt.Errorf("insert resource: %w", &mysql.MySQLError{Number: 1062})
	}
	return r.platformLLMGatewayRepoStub.CreateResource(item)
}

func TestEnsurePlatformLLMGatewayResourceConcurrentReplicas(t *testing.T) {
	repo := &concurrentPlatformGatewayRepo{platformLLMGatewayRepoStub: newPlatformLLMGatewayRepoStub(), ready: make(chan struct{})}
	services := []*openClawConfigService{{repo: repo}, {repo: repo}}
	var workers sync.WaitGroup
	for i := range 4 {
		workers.Go(func() {
			resource, err := services[i%2].EnsurePlatformLLMGatewayResource(9)
			if err != nil || resource == nil || resource.ID != 1 {
				t.Errorf("concurrent initialization: resource=%+v err=%v", resource, err)
			}
		})
	}
	workers.Wait()
	if len(repo.resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(repo.resources))
	}
}

type failedPlatformGatewayRepo struct {
	*platformLLMGatewayRepoStub
	err error
}

func (r *failedPlatformGatewayRepo) CreateResource(*models.OpenClawConfigResource) error {
	return r.err
}

func TestEnsurePlatformLLMGatewayResourcePreservesInsertFailure(t *testing.T) {
	for _, failure := range []error{errors.New("database unavailable"), &mysql.MySQLError{Number: 1062}} {
		repo := &failedPlatformGatewayRepo{platformLLMGatewayRepoStub: newPlatformLLMGatewayRepoStub(), err: failure}
		_, err := (&openClawConfigService{repo: repo}).EnsurePlatformLLMGatewayResource(9)
		if !errors.Is(err, failure) {
			t.Fatalf("lost insert failure: %v", err)
		}
	}
}
