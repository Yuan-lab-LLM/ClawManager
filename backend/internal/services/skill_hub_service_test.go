package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestIsHubPublishableBlob(t *testing.T) {
	tests := []struct {
		name string
		blob *models.SkillBlob
		want bool
	}{
		{
			name: "completed none risk with object key",
			blob: &models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "user/demo/hash.zip"},
			want: true,
		},
		{
			name: "completed low risk",
			blob: &models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskLow, ObjectKey: "key"},
			want: true,
		},
		{
			name: "medium risk blocked",
			blob: &models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskMedium, ObjectKey: "key"},
			want: false,
		},
		{
			name: "pending scan blocked",
			blob: &models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskNone, ObjectKey: "key"},
			want: false,
		},
		{
			name: "missing object key blocked",
			blob: &models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: ""},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHubPublishableBlob(tc.blob); got != tc.want {
				t.Fatalf("isHubPublishableBlob() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListMyHubSkillsExcludesDiscoveredSkills(t *testing.T) {
	svc := &skillService{
		repo: &skillRepoStub{
			skills: map[int]*models.Skill{
				1: {ID: 1, UserID: 1, SkillKey: "dogfood", Name: "dogfood", Status: skillStatusActive, SourceType: skillSourceUploaded},
				2: {ID: 2, UserID: 1, SkillKey: "software-development-spike", Name: "software-development/spike", Status: skillStatusActive, SourceType: skillSourceDiscovered},
			},
		},
	}
	items, err := svc.ListMyHubSkills(1)
	if err != nil {
		t.Fatalf("ListMyHubSkills() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1 uploaded skill only", len(items))
	}
	if items[0].SkillKey != "dogfood" {
		t.Fatalf("SkillKey = %q, want dogfood", items[0].SkillKey)
	}
}

func TestCanAttachSkillRules(t *testing.T) {
	svc := &skillService{}
	privateSkill := &models.Skill{UserID: 1, SourceType: skillSourceUploaded, Status: "active", Visibility: skillVisibilityPrivate, RiskLevel: skillRiskLow}
	publicSkill := &models.Skill{UserID: 2, SourceType: skillSourceUploaded, Status: "active", Visibility: skillVisibilityPublic, RiskLevel: skillRiskLow}
	ownInstance := &models.Instance{UserID: 1}
	otherInstance := &models.Instance{UserID: 3}

	if !svc.CanAttachSkill(1, "user", privateSkill, ownInstance) {
		t.Fatal("owner should attach private skill to own instance")
	}
	if svc.CanAttachSkill(3, "user", privateSkill, otherInstance) {
		t.Fatal("other user must not attach private skill")
	}
	if !svc.CanAttachSkill(3, "user", publicSkill, otherInstance) {
		t.Fatal("user should attach public skill to own instance")
	}
	if svc.CanAttachSkill(3, "user", publicSkill, ownInstance) {
		t.Fatal("user must not attach public skill to someone else's instance")
	}
	if !svc.CanAttachSkill(99, "admin", privateSkill, otherInstance) {
		t.Fatal("admin should attach any skill to any instance")
	}
}

func TestCanViewSkillRules(t *testing.T) {
	svc := &skillService{}
	privateSkill := &models.Skill{UserID: 1, SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate}
	publicSkill := &models.Skill{UserID: 2, SourceType: skillSourceUploaded, Visibility: skillVisibilityPublic}

	if !svc.CanViewSkill(1, "user", privateSkill) {
		t.Fatal("owner should view private skill")
	}
	if svc.CanViewSkill(3, "user", privateSkill) {
		t.Fatal("other user must not view private skill")
	}
	if !svc.CanViewSkill(3, "user", publicSkill) {
		t.Fatal("user should view public skill")
	}
	if !svc.CanViewSkill(99, "admin", privateSkill) {
		t.Fatal("admin should view private skill")
	}
}

func TestCanDownloadSkillRules(t *testing.T) {
	svc := &skillService{}
	privateSkill := &models.Skill{UserID: 1, SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate}
	publicSkill := &models.Skill{UserID: 2, SourceType: skillSourceUploaded, Visibility: skillVisibilityPublic}
	discoveredSkill := &models.Skill{UserID: 1, SourceType: skillSourceDiscovered, Visibility: skillVisibilityPublic}

	if !svc.CanDownloadSkill(1, "user", privateSkill) {
		t.Fatal("owner should download private uploaded skill")
	}
	if svc.CanDownloadSkill(3, "user", privateSkill) {
		t.Fatal("other user must not download private skill")
	}
	if !svc.CanDownloadSkill(3, "user", publicSkill) {
		t.Fatal("user should download public skill")
	}
	if svc.CanDownloadSkill(1, "user", discoveredSkill) {
		t.Fatal("discovered skill is not user-managed and must not download")
	}
}

func TestIsHubPublishableSkill(t *testing.T) {
	svc := &skillService{}
	blob := &models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "user/demo/hash.zip"}
	activeSkill := &models.Skill{SourceType: skillSourceUploaded, Status: "active"}
	inactiveSkill := &models.Skill{SourceType: skillSourceUploaded, Status: "inactive"}

	if !svc.isHubPublishable(activeSkill, blob) {
		t.Fatal("active uploaded skill with clean blob should be publishable")
	}
	if svc.isHubPublishable(inactiveSkill, blob) {
		t.Fatal("inactive skill must not be publishable")
	}
	if svc.isHubPublishable(activeSkill, &models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskNone, ObjectKey: "key"}) {
		t.Fatal("pending scan blob must block publish")
	}
}

func TestValidateHubTagSelection(t *testing.T) {
	svc := &skillService{repo: &hubTagRepoStub{
		tags: map[int]*models.SkillHubTag{
			1: {ID: 1, TagKey: "coding", Name: "Coding", AdminOnly: false},
			2: {ID: 2, TagKey: "featured", Name: "Featured", AdminOnly: true},
		},
	}}

	if err := svc.validateHubTagSelection("user", nil); err == nil || err.Error() != "skill_tags_required" {
		t.Fatalf("empty tag list should require tags, got %v", err)
	}
	if err := svc.validateHubTagSelection("user", []int{2}); err == nil || err.Error() != "access denied" {
		t.Fatalf("user must not select admin-only tag alone, got %v", err)
	}
	if err := svc.validateHubTagSelection("user", []int{1}); err != nil {
		t.Fatalf("user with public tag should pass, got %v", err)
	}
	if err := svc.validateHubTagSelection("admin", []int{2}); err == nil || err.Error() != "skill_tags_required" {
		t.Fatalf("admin-only tag alone should still require a public tag, got %v", err)
	}
	if err := svc.validateHubTagSelection("admin", []int{1, 2}); err != nil {
		t.Fatalf("admin with mixed tags should pass, got %v", err)
	}
}

func newPublishTestStub(blob *models.SkillBlob) (*skillService, *skillRepoStub) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, BlobID: blobID}},
		blobs:    map[int]*models.SkillBlob{blobID: blob},
		tags: map[int]*models.SkillHubTag{
			1: {ID: 1, TagKey: "coding", Name: "Coding", AdminOnly: false},
		},
		tagAssignments: map[int][]int{},
	}
	return &skillService{repo: stub}, stub
}

func TestDeleteSkillSoftPreservesInstanceSkills(t *testing.T) {
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive, SourceType: skillSourceUploaded, Visibility: skillVisibilityPublic},
		},
		tagAssignments: map[int][]int{1: {1}},
		instanceSkillsBySkillID: map[int][]models.InstanceSkill{
			1: {{ID: 99, InstanceID: 2, SkillID: 1, Status: "active"}},
		},
	}
	svc := &skillService{repo: stub}
	if err := svc.DeleteSkill(1, "user", 1); err != nil {
		t.Fatalf("DeleteSkill() error = %v", err)
	}
	if stub.hardDeleteCalled {
		t.Fatal("DeleteSkill must not hard-delete skill row")
	}
	if stub.skills[1].Status != skillStatusDeleted {
		t.Fatalf("expected soft-deleted status, got %q", stub.skills[1].Status)
	}
	if len(stub.tagAssignments[1]) != 0 {
		t.Fatalf("expected tag assignments cleared, got %v", stub.tagAssignments[1])
	}
	if len(stub.instanceSkillsBySkillID[1]) != 1 {
		t.Fatal("instance_skills records must remain after soft delete")
	}
}

func TestCanViewSkillDeletedHidden(t *testing.T) {
	svc := &skillService{}
	deletedSkill := &models.Skill{UserID: 1, SourceType: skillSourceUploaded, Visibility: skillVisibilityPublic, Status: skillStatusDeleted}
	if svc.CanViewSkill(3, "user", deletedSkill) {
		t.Fatal("deleted public skill must not be viewable")
	}
}

func TestPublishToHubRejectsPendingScan(t *testing.T) {
	svc, _ := newPublishTestStub(&models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskNone, ObjectKey: "key.zip"})
	_, err := svc.PublishToHub(1, "user", 1, []int{1})
	if err == nil || err.Error() != "skill_not_scanned" {
		t.Fatalf("expected skill_not_scanned, got %v", err)
	}
}

func TestPublishToHubRejectsMediumRisk(t *testing.T) {
	svc, _ := newPublishTestStub(&models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskMedium, ObjectKey: "key.zip"})
	_, err := svc.PublishToHub(1, "user", 1, []int{1})
	if err == nil || err.Error() != "skill_risk_blocked" {
		t.Fatalf("expected skill_risk_blocked, got %v", err)
	}
}

func TestPublishToHubRejectsEmptyTags(t *testing.T) {
	svc, _ := newPublishTestStub(&models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "key.zip"})
	_, err := svc.PublishToHub(1, "user", 1, nil)
	if err == nil || err.Error() != "skill_tags_required" {
		t.Fatalf("expected skill_tags_required, got %v", err)
	}
}

func TestPublishToHubAllowsAdminForOtherUsersSkill(t *testing.T) {
	svc, _ := newPublishTestStub(&models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "key.zip"})
	item, err := svc.PublishToHub(99, "admin", 1, []int{1})
	if err != nil {
		t.Fatalf("PublishToHub() error = %v", err)
	}
	if item == nil || !strings.EqualFold(item.Visibility, skillVisibilityPublic) {
		t.Fatalf("expected admin publish to succeed, got %#v", item)
	}
}

func TestUnpublishAllowsAdminForOtherUsersSkill(t *testing.T) {
	svc, stub := newPublishTestStub(&models.SkillBlob{ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "key.zip"})
	stub.skills[1].Visibility = skillVisibilityPublic
	item, err := svc.UnpublishFromHub(99, "admin", 1)
	if err != nil {
		t.Fatalf("UnpublishFromHub() error = %v", err)
	}
	if item == nil || !strings.EqualFold(item.Visibility, skillVisibilityPrivate) {
		t.Fatalf("expected admin unpublish to succeed, got %#v", item)
	}
}

func TestUnpublishRemovesFromPublicCatalog(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPublic, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{10: {ID: 10, BlobID: blobID}},
		blobs:    map[int]*models.SkillBlob{blobID: {ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "key.zip"}},
	}
	svc := &skillService{repo: stub}
	if _, err := svc.UnpublishFromHub(1, "user", 1); err != nil {
		t.Fatalf("UnpublishFromHub() error = %v", err)
	}
	if stub.skills[1].Visibility != skillVisibilityPrivate {
		t.Fatalf("expected private visibility after unpublish, got %q", stub.skills[1].Visibility)
	}
	publicSkills, err := stub.ListPublicHubSkills()
	if err != nil {
		t.Fatalf("ListPublicHubSkills() error = %v", err)
	}
	if len(publicSkills) != 0 {
		t.Fatalf("catalog source should be empty after unpublish, got %d items", len(publicSkills))
	}
}

func TestListAllHubSkillsAdminExcludesDeleted(t *testing.T) {
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive, SourceType: skillSourceUploaded, Visibility: skillVisibilityPublic},
			2: {ID: 2, UserID: 1, SkillKey: "demo__deleted_2", Name: "Demo", Status: skillStatusDeleted, SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate},
		},
		tagAssignments: map[int][]int{},
	}
	svc := &skillService{repo: stub}
	items, err := svc.ListAllHubSkillsAdmin()
	if err != nil {
		t.Fatalf("ListAllHubSkillsAdmin() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListAllHubSkillsAdmin() len = %d, want 1", len(items))
	}
	if items[0].ID != 1 {
		t.Fatalf("ListAllHubSkillsAdmin() id = %d, want 1", items[0].ID)
	}
}

type importTestInstanceRepo struct {
	instances map[int]*models.Instance
}

func (r *importTestInstanceRepo) Create(*models.Instance) error { panic("not used") }
func (r *importTestInstanceRepo) GetByID(id int) (*models.Instance, error) {
	if inst, ok := r.instances[id]; ok {
		copy := *inst
		return &copy, nil
	}
	return nil, nil
}
func (r *importTestInstanceRepo) GetByAccessToken(string) (*models.Instance, error) { panic("not used") }
func (r *importTestInstanceRepo) GetByAgentBootstrapToken(string) (*models.Instance, error) {
	panic("not used")
}
func (r *importTestInstanceRepo) GetAll(int, int) ([]models.Instance, error) {
	items := make([]models.Instance, 0, len(r.instances))
	for _, inst := range r.instances {
		items = append(items, *inst)
	}
	return items, nil
}
func (r *importTestInstanceRepo) CountAll() (int, error)                    { panic("not used") }
func (r *importTestInstanceRepo) GetByUserID(int, int, int) ([]models.Instance, error) {
	panic("not used")
}
func (r *importTestInstanceRepo) CountByUserID(int) (int, error) { panic("not used") }
func (r *importTestInstanceRepo) CountActiveByMode(context.Context, string) (int, error) {
	panic("not used")
}
func (r *importTestInstanceRepo) ExistsByUserIDAndName(int, string) (bool, error) {
	panic("not used")
}
func (r *importTestInstanceRepo) GetAllRunning() ([]models.Instance, error) { panic("not used") }
func (r *importTestInstanceRepo) GetV2DesiredRunning(context.Context, int) ([]models.Instance, error) {
	panic("not used")
}
func (r *importTestInstanceRepo) GetV2Creating(context.Context, int) ([]models.Instance, error) {
	panic("not used")
}
func (r *importTestInstanceRepo) UpdateRuntimeState(context.Context, int, string, int, *string) error {
	panic("not used")
}
func (r *importTestInstanceRepo) SetWorkspacePath(context.Context, int, string) error {
	panic("not used")
}
func (r *importTestInstanceRepo) UpdateWorkspaceUsage(context.Context, int, int64) error {
	panic("not used")
}
func (r *importTestInstanceRepo) Update(*models.Instance) error             { panic("not used") }
func (r *importTestInstanceRepo) Delete(int) error                          { panic("not used") }

type noopInstanceCommandService struct{}

func (n *noopInstanceCommandService) Create(int, *int, CreateInstanceCommandRequest) (*InstanceCommandPayload, error) {
	return nil, nil
}
func (n *noopInstanceCommandService) GetNextForAgent(*AgentSession) (*AgentCommandEnvelope, error) {
	panic("not used")
}
func (n *noopInstanceCommandService) MarkStarted(*AgentSession, int, *time.Time) error {
	panic("not used")
}
func (n *noopInstanceCommandService) MarkFinished(*AgentSession, int, AgentCommandFinishRequest) error {
	panic("not used")
}
func (n *noopInstanceCommandService) ListByInstanceID(int, int) ([]InstanceCommandPayload, error) {
	panic("not used")
}

func TestPublishFromInstanceRejectsDiscoveredSkill(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceDiscovered, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID}},
		blobs:    map[int]*models.SkillBlob{blobID: {ID: blobID, ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "key.zip"}},
		instanceSkills: []models.InstanceSkill{{InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance"}},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}
	_, err := svc.PublishFromInstance(1, "user", 1, 1, []int{1})
	if err == nil || err.Error() != "skill_not_in_library" {
		t.Fatalf("expected skill_not_in_library, got %v", err)
	}
}

func TestImportInstanceSkillToLibraryPendingPackage(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceDiscovered, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID}},
		blobs:    map[int]*models.SkillBlob{blobID: {ID: blobID, ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""}},
		instanceSkills: []models.InstanceSkill{{InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance"}},
		tagAssignments: map[int][]int{},
		tags: map[int]*models.SkillHubTag{
			1: {ID: 1, TagKey: "coding", Name: "Coding", AdminOnly: false},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}
	_, err := svc.ImportInstanceSkillToLibrary(1, "user", 1, 1)
	if err == nil || err.Error() != "skill_package_pending" {
		t.Fatalf("expected skill_package_pending, got %v", err)
	}
}

func TestImportInstanceSkillToLibraryRejectsNonOwner(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceDiscovered, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions:       map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID}},
		blobs:          map[int]*models.SkillBlob{blobID: {ID: blobID, ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""}},
		instanceSkills: []models.InstanceSkill{{InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance"}},
		tagAssignments: map[int][]int{},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}
	_, err := svc.ImportInstanceSkillToLibrary(2, "user", 1, 1)
	if err == nil || err.Error() != "access denied" {
		t.Fatalf("expected access denied, got %v", err)
	}
}

func TestImportInstanceSkillToLibraryRejectsDeletedSkill(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusDeleted,
				SourceType: skillSourceDiscovered, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions:       map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID}},
		blobs:          map[int]*models.SkillBlob{blobID: {ID: blobID, ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: "discovered/1/demo.zip"}},
		instanceSkills: []models.InstanceSkill{{InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance"}},
		tagAssignments: map[int][]int{},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}
	_, err := svc.ImportInstanceSkillToLibrary(1, "user", 1, 1)
	if err == nil || err.Error() != "skill not found" {
		t.Fatalf("expected skill not found, got %v", err)
	}
}

func TestImportInstanceSkillToLibraryPromotesDiscoveredSkill(t *testing.T) {
	versionID := 10
	blobID := 20
	scanResultID := 99
	objectKey := "discovered/1/demo.zip"
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceDiscovered, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID, SourceType: skillSourceDiscovered}},
		blobs: map[int]*models.SkillBlob{
			blobID: {
				ID: blobID, ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: objectKey,
				LastScanResultID: &scanResultID,
			},
		},
		instanceSkills: []models.InstanceSkill{{InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance"}},
		tagAssignments: map[int][]int{},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	storage := &importTestObjectStorage{objects: map[string][]byte{objectKey: []byte("fake-zip")}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}, storage: storage}
	_, err := svc.ImportInstanceSkillToLibrary(1, "user", 1, 1)
	if err != nil {
		t.Fatalf("ImportInstanceSkillToLibrary() error = %v", err)
	}
	if stub.skills[1].SourceType != skillSourceUploaded {
		t.Fatalf("skill source_type = %q, want %q", stub.skills[1].SourceType, skillSourceUploaded)
	}
	if stub.versions[versionID].SourceType != skillSourceUploaded {
		t.Fatalf("version source_type = %q, want %q", stub.versions[versionID].SourceType, skillSourceUploaded)
	}
}

func TestImportInstanceSkillToLibraryLiteMaterializesPackage(t *testing.T) {
	versionID := 10
	blobID := 20
	contentHash := "abc123def456789012345678901234"
	workspaceDir := "yuanbao"
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "yuanbao", Name: "yuanbao", Status: skillStatusActive,
				SourceType: skillSourceDiscovered, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID, SourceType: skillSourceDiscovered}},
		blobs: map[int]*models.SkillBlob{
			blobID: {ID: blobID, ContentHash: contentHash, ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""},
		},
		instanceSkills: []models.InstanceSkill{{
			InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance",
			WorkspaceDir: &workspaceDir,
		}},
		tagAssignments: map[int][]int{},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{
		1: {ID: 1, UserID: 1, InstanceMode: InstanceModeLite, RuntimeType: RuntimeBackendGateway},
	}}
	storage := &importTestObjectStorage{objects: map[string][]byte{}}
	cmdSvc := &capturingInstanceCommandService{}
	matSvc := NewSkillPackageMaterializeService(
		&materializeJobRepoStub{},
		stub,
		importLiteMaterializer{repo: stub, storage: storage, blobID: blobID},
	)
	svc := &skillService{
		repo: stub, instanceRepo: instRepo, commandService: cmdSvc, storage: storage, materializeService: matSvc,
	}
	_, err := svc.ImportInstanceSkillToLibrary(1, "user", 1, 1)
	if err != nil {
		t.Fatalf("ImportInstanceSkillToLibrary() error = %v", err)
	}
	for _, req := range cmdSvc.created {
		if req.CommandType == InstanceCommandTypeCollectSkillPackage {
			t.Fatalf("unexpected collect_skill_package command: %#v", req)
		}
	}
	if stub.skills[1].SourceType != skillSourceUploaded {
		t.Fatalf("skill source_type = %q, want %q", stub.skills[1].SourceType, skillSourceUploaded)
	}
	blob := stub.blobs[blobID]
	if blob == nil || strings.TrimSpace(blob.ObjectKey) == "" {
		t.Fatalf("expected materialized object key, got %#v", blob)
	}
}

func TestImportInstanceSkillToLibraryIdempotentForUploaded(t *testing.T) {
	versionID := 10
	blobID := 20
	scanResultID := 99
	objectKey := "user/1/demo.zip"
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID, SourceType: skillSourceUploaded}},
		blobs: map[int]*models.SkillBlob{
			blobID: {
				ID: blobID, ScanStatus: "completed", RiskLevel: skillRiskNone, ObjectKey: objectKey,
				LastScanResultID: &scanResultID,
			},
		},
		instanceSkills: []models.InstanceSkill{{InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance"}},
		tagAssignments: map[int][]int{},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	storage := &importTestObjectStorage{objects: map[string][]byte{objectKey: []byte("fake-zip")}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}, storage: storage}
	payload, err := svc.ImportInstanceSkillToLibrary(1, "user", 1, 1)
	if err != nil {
		t.Fatalf("ImportInstanceSkillToLibrary() error = %v", err)
	}
	if stub.skills[1].SourceType != skillSourceUploaded {
		t.Fatalf("skill source_type = %q, want %q", stub.skills[1].SourceType, skillSourceUploaded)
	}
	if payload == nil || payload.SourceType != skillSourceUploaded {
		t.Fatalf("payload source_type = %v, want %q", payload, skillSourceUploaded)
	}
}

func TestSyncAgentSkillsCreatesDiscoveredSkillWithPrivateVisibility(t *testing.T) {
	stub := &capturingSkillRepoStub{
		skillRepoStub: skillRepoStub{
			skills:   map[int]*models.Skill{},
			blobs:    map[int]*models.SkillBlob{},
			versions: map[int]*models.SkillVersion{},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}
	err := svc.SyncAgentSkills(1, AgentSkillInventoryReportRequest{
		Skills: []AgentSkillRecord{{
			Identifier: "weather",
			ContentMD5: "abc123def456789012345678901234",
			Source:     "discovered_in_instance",
		}},
	})
	if err != nil {
		t.Fatalf("SyncAgentSkills() error = %v", err)
	}
	if len(stub.createdSkills) != 1 {
		t.Fatalf("created %d skills, want 1", len(stub.createdSkills))
	}
	if stub.createdSkills[0].Visibility != skillVisibilityPrivate {
		t.Fatalf("visibility = %q, want %q", stub.createdSkills[0].Visibility, skillVisibilityPrivate)
	}
}

type capturingSkillRepoStub struct {
	skillRepoStub
	createdSkills         []*models.Skill
	nextSkillID           int
	markMissingCalls      int
	lastMarkMissingActive []int
}

func (s *capturingSkillRepoStub) CreateSkill(skill *models.Skill) error {
	s.nextSkillID++
	skill.ID = s.nextSkillID
	copy := *skill
	s.createdSkills = append(s.createdSkills, &copy)
	if s.skills == nil {
		s.skills = map[int]*models.Skill{}
	}
	stored := *skill
	s.skills[skill.ID] = &stored
	return nil
}

func (s *capturingSkillRepoStub) CreateBlob(blob *models.SkillBlob) error {
	if s.blobs == nil {
		s.blobs = map[int]*models.SkillBlob{}
	}
	s.nextSkillID++
	blob.ID = s.nextSkillID
	stored := *blob
	s.blobs[blob.ID] = &stored
	return nil
}

func (s *capturingSkillRepoStub) CreateVersion(version *models.SkillVersion) error {
	if s.versions == nil {
		s.versions = map[int]*models.SkillVersion{}
	}
	s.nextSkillID++
	version.ID = s.nextSkillID
	stored := *version
	s.versions[version.ID] = &stored
	return nil
}

func (s *capturingSkillRepoStub) GetBlobByContentHash(hash string) (*models.SkillBlob, error) {
	for _, blob := range s.blobs {
		if blob != nil && blob.ContentHash == hash {
			copy := *blob
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *capturingSkillRepoStub) GetVersionBySkillAndBlob(skillID, blobID int) (*models.SkillVersion, error) {
	for _, version := range s.versions {
		if version != nil && version.SkillID == skillID && version.BlobID == blobID {
			copy := *version
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *capturingSkillRepoStub) UpsertInstanceSkill(item *models.InstanceSkill) error {
	copy := *item
	updated := false
	for i, existing := range s.instanceSkills {
		if existing.InstanceID == item.InstanceID && existing.SkillID == item.SkillID {
			s.instanceSkills[i] = copy
			updated = true
			break
		}
	}
	if !updated {
		s.instanceSkills = append(s.instanceSkills, copy)
	}
	return nil
}

func (s *capturingSkillRepoStub) MarkMissingInstanceSkills(instanceID int, activeSkillIDs []int, observedAt time.Time) error {
	s.markMissingCalls++
	s.lastMarkMissingActive = append([]int(nil), activeSkillIDs...)
	active := map[int]struct{}{}
	for _, id := range activeSkillIDs {
		active[id] = struct{}{}
	}
	for i := range s.instanceSkills {
		item := &s.instanceSkills[i]
		if item.InstanceID != instanceID {
			continue
		}
		if _, ok := active[item.SkillID]; ok {
			continue
		}
		if strings.EqualFold(item.Status, "removed") {
			continue
		}
		item.Status = "missing"
		item.RemovedAt = &observedAt
		item.UpdatedAt = observedAt
	}
	return nil
}

type importLiteMaterializer struct {
	repo    *skillRepoStub
	storage *importTestObjectStorage
	blobID  int
}

func (m importLiteMaterializer) materializeSkillPackageFromWorkspace(_ context.Context, instanceID int, workspaceDir, contentHash string, _ int) (*models.SkillBlob, error) {
	objectKey := fmt.Sprintf("discovered/%d/%s/%s.zip", instanceID, workspaceDir, contentHash)
	if m.storage.objects == nil {
		m.storage.objects = map[string][]byte{}
	}
	m.storage.objects[objectKey] = []byte("fake-zip")
	scanID := 99
	blob := m.repo.blobs[m.blobID]
	blob.ObjectKey = objectKey
	blob.ScanStatus = "completed"
	blob.RiskLevel = skillRiskNone
	blob.LastScanResultID = &scanID
	m.repo.blobs[m.blobID] = blob
	return blob, nil
}

func (importLiteMaterializer) syncSkillRecordFromBlob(int, *models.SkillBlob) error { return nil }

type importTestObjectStorage struct {
	objects map[string][]byte
}

func (s *importTestObjectStorage) PutObject(_ context.Context, objectKey string, body []byte, _ string) error {
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[objectKey] = body
	return nil
}

func (s *importTestObjectStorage) GetObject(_ context.Context, objectKey string) ([]byte, error) {
	if body, ok := s.objects[objectKey]; ok {
		return body, nil
	}
	return nil, fmt.Errorf("object not found: %s", objectKey)
}

func TestDeleteSkillReleasesSkillKey(t *testing.T) {
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {ID: 1, UserID: 1, SkillKey: "weather", Name: "Weather", Status: skillStatusActive, SourceType: skillSourceUploaded},
		},
		tagAssignments: map[int][]int{},
	}
	svc := &skillService{repo: stub}
	if err := svc.DeleteSkill(1, "user", 1); err != nil {
		t.Fatalf("DeleteSkill() error = %v", err)
	}
	want := deletedSkillKey("weather", 1)
	if stub.skills[1].SkillKey != want {
		t.Fatalf("skill_key = %q, want %q", stub.skills[1].SkillKey, want)
	}
	active, err := stub.GetSkillByUserKey(1, "weather")
	if err != nil {
		t.Fatalf("GetSkillByUserKey() error = %v", err)
	}
	if active != nil {
		t.Fatal("original skill_key should be released for re-import")
	}
}

func TestSyncAgentSkillsIncrementalDoesNotMarkMissing(t *testing.T) {
	contentHash := "abc123def456789012345678901234"
	versionID := 1
	stub := &capturingSkillRepoStub{
		skillRepoStub: skillRepoStub{
			skills: map[int]*models.Skill{
				10: {
					ID: 10, UserID: 1, SkillKey: "weather", Name: "weather",
					SourceType: skillSourceDiscovered, Status: skillStatusActive,
					Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
				},
			},
			blobs: map[int]*models.SkillBlob{
				1: {ID: 1, ContentHash: contentHash, ObjectKey: "discovered/weather.zip", ScanStatus: "completed"},
			},
			versions: map[int]*models.SkillVersion{
				1: {ID: 1, SkillID: 10, BlobID: 1, VersionNo: 1},
			},
			instanceSkills: []models.InstanceSkill{
				{InstanceID: 1, SkillID: 10, Status: "active", SourceType: "discovered_in_instance"},
			},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}

	if err := svc.SyncAgentSkills(1, AgentSkillInventoryReportRequest{Mode: "incremental", Skills: nil}); err != nil {
		t.Fatalf("incremental SyncAgentSkills() error = %v", err)
	}
	if stub.markMissingCalls != 0 {
		t.Fatalf("markMissingCalls = %d, want 0 for incremental", stub.markMissingCalls)
	}
	if stub.instanceSkills[0].Status != "active" {
		t.Fatalf("status = %q, want active after incremental empty report", stub.instanceSkills[0].Status)
	}

	if err := svc.SyncAgentSkills(1, AgentSkillInventoryReportRequest{Mode: "full", Skills: nil}); err != nil {
		t.Fatalf("full SyncAgentSkills() error = %v", err)
	}
	if stub.markMissingCalls != 1 {
		t.Fatalf("markMissingCalls = %d, want 1 for full", stub.markMissingCalls)
	}
	if stub.instanceSkills[0].Status != "missing" {
		t.Fatalf("status = %q, want missing after full empty report", stub.instanceSkills[0].Status)
	}
}

func TestSyncAgentSkillsReactivatesMissingButNotRemoved(t *testing.T) {
	contentHash := "abc123def456789012345678901234"
	versionID := 1
	removedAt := time.Now().UTC().Add(-time.Hour)
	stub := &capturingSkillRepoStub{
		skillRepoStub: skillRepoStub{
			skills: map[int]*models.Skill{
				10: {
					ID: 10, UserID: 1, SkillKey: "weather", Name: "weather",
					SourceType: skillSourceDiscovered, Status: skillStatusActive,
					Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
				},
				11: {
					ID: 11, UserID: 1, SkillKey: "calendar", Name: "calendar",
					SourceType: skillSourceDiscovered, Status: skillStatusActive,
					Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
				},
			},
			blobs: map[int]*models.SkillBlob{
				1: {ID: 1, ContentHash: contentHash, ObjectKey: "discovered/weather.zip", ScanStatus: "completed"},
				2: {ID: 2, ContentHash: "def456abc123789012345678901234", ObjectKey: "discovered/calendar.zip", ScanStatus: "completed"},
			},
			versions: map[int]*models.SkillVersion{
				1: {ID: 1, SkillID: 10, BlobID: 1, VersionNo: 1},
				2: {ID: 2, SkillID: 11, BlobID: 2, VersionNo: 1},
			},
			instanceSkills: []models.InstanceSkill{
				{InstanceID: 1, SkillID: 10, Status: "missing", SourceType: "discovered_in_instance", RemovedAt: &removedAt},
				{InstanceID: 1, SkillID: 11, Status: "removed", SourceType: "discovered_in_instance", RemovedAt: &removedAt},
			},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: {ID: 1, UserID: 1}}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, commandService: &noopInstanceCommandService{}}

	err := svc.SyncAgentSkills(1, AgentSkillInventoryReportRequest{
		Mode: "full",
		Skills: []AgentSkillRecord{
			{Identifier: "weather", ContentMD5: contentHash, Source: "discovered_in_instance"},
			{Identifier: "calendar", ContentMD5: "def456abc123789012345678901234", Source: "discovered_in_instance"},
		},
	})
	if err != nil {
		t.Fatalf("SyncAgentSkills() error = %v", err)
	}

	var missing, removed *models.InstanceSkill
	for i := range stub.instanceSkills {
		item := &stub.instanceSkills[i]
		switch item.SkillID {
		case 10:
			missing = item
		case 11:
			removed = item
		}
	}
	if missing == nil || missing.Status != "active" || missing.RemovedAt != nil {
		t.Fatalf("missing skill revive = %#v, want active with nil RemovedAt", missing)
	}
	if removed == nil || removed.Status != "removed" {
		t.Fatalf("removed skill = %#v, want status removed", removed)
	}
}

func TestDownloadSkillNilSafe(t *testing.T) {
	missingVersionID := 999
	versionID := 7
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {ID: 1, UserID: 1, Status: skillStatusActive, Visibility: skillVisibilityPrivate, CurrentVersionID: &missingVersionID},
			2: {ID: 2, UserID: 1, Status: skillStatusActive, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID},
		},
		versions: map[int]*models.SkillVersion{
			7: {ID: 7, SkillID: 2, BlobID: 99},
		},
		blobs: map[int]*models.SkillBlob{},
	}
	svc := &skillService{repo: stub, storage: &importTestObjectStorage{objects: map[string][]byte{}}}

	if _, _, err := svc.DownloadSkill(1, "user", 1); err == nil {
		t.Fatal("expected error when current version is missing")
	}
	if _, _, err := svc.DownloadSkill(1, "user", 2); err == nil {
		t.Fatal("expected error when blob is missing")
	}
}
