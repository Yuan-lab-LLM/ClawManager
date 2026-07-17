package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestMaterializeBlockedReason(t *testing.T) {
	running := "skill_package_materializing"
	if got := materializeBlockedReason(&models.SkillPackageMaterializeJob{Status: MaterializeJobStatusRunning}); got == nil || *got != running {
		t.Fatalf("running reason = %v, want %q", got, running)
	}
	failed := "skill_package_materialize_failed"
	if got := materializeBlockedReason(&models.SkillPackageMaterializeJob{Status: MaterializeJobStatusFailed}); got == nil || *got != failed {
		t.Fatalf("failed reason = %v, want %q", got, failed)
	}
}

func TestPublishBlockedReasonUsesMaterializeJob(t *testing.T) {
	svc := &skillService{
		materializeService: NewSkillPackageMaterializeService(
			&materializeJobRepoStub{latest: &models.SkillPackageMaterializeJob{Status: MaterializeJobStatusPending}},
			nil,
			nil,
		),
	}
	skill := &models.Skill{ID: 1, Status: skillStatusActive, SourceType: skillSourceDiscovered}
	blob := &models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""}

	reason := svc.publishBlockedReasonForSkill(skill, blob, nil, false, false)
	if reason == nil || *reason != "skill_package_materializing" {
		t.Fatalf("expected skill_package_materializing, got %v", reason)
	}
}

func TestPublishBlockedReasonSkipsCollectWhenMaterializeJobSucceeded(t *testing.T) {
	svc := &skillService{
		materializeService: NewSkillPackageMaterializeService(
			&materializeJobRepoStub{latest: &models.SkillPackageMaterializeJob{Status: MaterializeJobStatusSucceeded}},
			nil,
			nil,
		),
		commandRepo: &stubCommandRepo{
			failed: &models.InstanceCommand{
				CommandType:  "collect_skill_package",
				Status:       "failed",
				ErrorMessage: strPtr("agent failed"),
			},
		},
	}
	skill := &models.Skill{ID: 1, Status: skillStatusActive, SourceType: skillSourceDiscovered}
	blob := &models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""}

	reason := svc.publishBlockedReasonForSkill(skill, blob, nil, false, false)
	if reason == nil || *reason != "skill_package_pending" {
		t.Fatalf("expected skill_package_pending, got %v", reason)
	}
}

func TestPublishBlockedReasonLiteSkipsAgentCollectFailed(t *testing.T) {
	svc := &skillService{
		commandRepo: &stubCommandRepo{
			failed: &models.InstanceCommand{
				CommandType:  "collect_skill_package",
				Status:       "failed",
				ErrorMessage: strPtr("agent failed"),
			},
		},
	}
	skill := &models.Skill{ID: 1, Status: skillStatusActive, SourceType: skillSourceDiscovered}
	blob := &models.SkillBlob{ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""}

	reason := svc.publishBlockedReasonForSkill(skill, blob, nil, false, true)
	if reason == nil || *reason != "skill_package_pending" {
		t.Fatalf("expected skill_package_pending, got %v", reason)
	}
	collectErr := svc.resolvePackageCollectError(skill.ID, blob, nil, true)
	if collectErr != nil {
		t.Fatalf("expected nil collect error for lite, got %v", collectErr)
	}
}

func TestListMyHubSkillsLiteAutoResolvesInstanceContext(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "yuanbao", Name: "yuanbao", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID}},
		blobs: map[int]*models.SkillBlob{
			blobID: {ID: blobID, ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""},
		},
		instanceSkills: []models.InstanceSkill{{
			InstanceID: 1, SkillID: 1, Status: "active", SourceType: "discovered_in_instance",
		}},
		tagAssignments: map[int][]int{},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{
		1: {ID: 1, UserID: 1, InstanceMode: InstanceModeLite, RuntimeType: RuntimeBackendGateway},
	}}
	svc := &skillService{
		repo:         stub,
		instanceRepo: instRepo,
		commandRepo: &stubCommandRepo{
			failed: &models.InstanceCommand{
				CommandType:  "collect_skill_package",
				Status:       "failed",
				ErrorMessage: strPtr("agent failed"),
			},
		},
	}
	items, err := svc.ListMyHubSkills(1)
	if err != nil {
		t.Fatalf("ListMyHubSkills() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].PublishBlockedReason == nil || *items[0].PublishBlockedReason != "skill_package_pending" {
		t.Fatalf("PublishBlockedReason = %v, want skill_package_pending", items[0].PublishBlockedReason)
	}
	if items[0].PackageCollectError != nil {
		t.Fatalf("PackageCollectError = %v, want nil", items[0].PackageCollectError)
	}
}

func TestSyncAgentSkillsLiteSkipsAgentEnqueue(t *testing.T) {
	stub := &capturingSkillRepoStub{
		skillRepoStub: skillRepoStub{
			skills:   map[int]*models.Skill{},
			blobs:    map[int]*models.SkillBlob{},
			versions: map[int]*models.SkillVersion{},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{
		1: {ID: 1, UserID: 1, InstanceMode: InstanceModeLite, RuntimeType: RuntimeBackendGateway},
	}}
	cmdSvc := &capturingInstanceCommandService{}
	matSvc := NewSkillPackageMaterializeService(&materializeJobRepoStub{}, nil, nil)
	svc := &skillService{
		repo:               stub,
		instanceRepo:       instRepo,
		commandService:     cmdSvc,
		materializeService: matSvc,
	}
	err := svc.SyncAgentSkills(1, AgentSkillInventoryReportRequest{
		Skills: []AgentSkillRecord{{
			Identifier:  "yuanbao",
			ContentMD5:  "abc123def456789012345678901234",
			Source:      "discovered_in_instance",
			InstallPath: "home/.hermes/skills/yuanbao",
		}},
	})
	if err != nil {
		t.Fatalf("SyncAgentSkills() error = %v", err)
	}
	for _, req := range cmdSvc.created {
		if req.CommandType == InstanceCommandTypeCollectSkillPackage {
			t.Fatalf("unexpected collect_skill_package command: %#v", req)
		}
	}
}

func TestEnqueueSkipsWhenObjectKeyPresent(t *testing.T) {
	objectKey := "discovered/1/demo/abc.zip"
	service := NewSkillPackageMaterializeService(
		&materializeJobRepoStub{},
		&materializeBlobRepoStub{
			blobs: map[int]*models.SkillBlob{
				20: {ID: 20, ObjectKey: objectKey, ContentHash: "abc"},
			},
		},
		nil,
	)
	job, err := service.Enqueue(context.Background(), EnqueueMaterializeRequest{
		InstanceID:     1,
		SkillID:        1,
		BlobID:         20,
		WorkspaceDir:   "demo",
		ContentHash:    "abc",
		TriggerSource:  MaterializeTriggerSync,
		IdempotencyKey: "materialize-1-abc",
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if job == nil || job.Status != MaterializeJobStatusSucceeded {
		t.Fatalf("expected succeeded job, got %#v", job)
	}
}

func TestEnqueueRequeuesFailedMaterializeJob(t *testing.T) {
	failed := "skill package md5 mismatch"
	job := &models.SkillPackageMaterializeJob{
		ID: 7, InstanceID: 1, SkillID: 1, BlobID: 20, WorkspaceDir: "demo",
		ContentHash: "stale", Status: MaterializeJobStatusFailed, LastError: &failed,
		IdempotencyKey: "materialize-1-goodhash",
	}
	repo := &materializeJobRepoStub{
		latest: job,
		byKey:  map[string]*models.SkillPackageMaterializeJob{"materialize-1-goodhash": job},
	}
	service := NewSkillPackageMaterializeService(repo, nil, nil)
	updated, err := service.Enqueue(context.Background(), EnqueueMaterializeRequest{
		InstanceID:     1,
		SkillID:        1,
		BlobID:         20,
		WorkspaceDir:   "demo",
		ContentHash:    "goodhash",
		IdempotencyKey: "materialize-1-goodhash",
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if updated == nil || updated.Status != MaterializeJobStatusPending {
		t.Fatalf("expected pending job, got %#v", updated)
	}
	if updated.ContentHash != "goodhash" {
		t.Fatalf("content_hash = %q, want goodhash", updated.ContentHash)
	}
}

func TestNewSkillPackageMaterializeWorkerDefaults(t *testing.T) {
	worker := NewSkillPackageMaterializeWorker(nil, 0, 0, 0, 0, true)
	if worker.perInstanceLimit != 2 {
		t.Fatalf("perInstanceLimit = %d, want 2", worker.perInstanceLimit)
	}
	if worker.concurrency != 5 {
		t.Fatalf("concurrency = %d, want 5", worker.concurrency)
	}
}

type capturingInstanceCommandService struct {
	created []CreateInstanceCommandRequest
}

func (c *capturingInstanceCommandService) Create(_ int, _ *int, req CreateInstanceCommandRequest) (*InstanceCommandPayload, error) {
	c.created = append(c.created, req)
	return &InstanceCommandPayload{CommandType: req.CommandType, Status: "pending"}, nil
}
func (c *capturingInstanceCommandService) GetNextForAgent(*AgentSession) (*AgentCommandEnvelope, error) {
	return nil, nil
}
func (c *capturingInstanceCommandService) MarkStarted(*AgentSession, int, *time.Time) error { return nil }
func (c *capturingInstanceCommandService) MarkFinished(*AgentSession, int, AgentCommandFinishRequest) error {
	return nil
}
func (c *capturingInstanceCommandService) ListByInstanceID(int, int) ([]InstanceCommandPayload, error) {
	return nil, nil
}

type materializeJobRepoStub struct {
	latest  *models.SkillPackageMaterializeJob
	created []*models.SkillPackageMaterializeJob
	byKey   map[string]*models.SkillPackageMaterializeJob
}

func (s *materializeJobRepoStub) Create(job *models.SkillPackageMaterializeJob) error {
	s.created = append(s.created, job)
	if job.ID == 0 {
		job.ID = len(s.created)
	}
	return nil
}
func (s *materializeJobRepoStub) GetByID(id int) (*models.SkillPackageMaterializeJob, error) {
	for _, job := range s.created {
		if job.ID == id {
			return job, nil
		}
	}
	return s.latest, nil
}
func (s *materializeJobRepoStub) GetByIdempotencyKey(key string) (*models.SkillPackageMaterializeJob, error) {
	if s.byKey != nil {
		if job, ok := s.byKey[key]; ok {
			return job, nil
		}
	}
	return nil, nil
}
func (s *materializeJobRepoStub) ClaimNextPending(context.Context, int) ([]models.SkillPackageMaterializeJob, error) {
	return nil, nil
}
func (s *materializeJobRepoStub) MarkSucceeded(id int) error {
	for _, job := range s.created {
		if job.ID == id {
			job.Status = MaterializeJobStatusSucceeded
		}
	}
	return nil
}
func (s *materializeJobRepoStub) MarkFailed(id int, msg string, _ bool) error {
	for _, job := range s.created {
		if job.ID == id {
			job.Status = MaterializeJobStatusFailed
			job.LastError = &msg
		}
	}
	if s.latest != nil && s.latest.ID == id {
		s.latest.Status = MaterializeJobStatusFailed
		s.latest.LastError = &msg
	}
	return nil
}
func (s *materializeJobRepoStub) MarkRunning(id int) error {
	for _, job := range s.created {
		if job.ID == id {
			job.Status = MaterializeJobStatusRunning
		}
	}
	if s.latest != nil && s.latest.ID == id {
		s.latest.Status = MaterializeJobStatusRunning
	}
	return nil
}
func (s *materializeJobRepoStub) ReleaseToPending(int) error          { return nil }
func (s *materializeJobRepoStub) ResetForRetry(int) error             { return nil }
func (s *materializeJobRepoStub) RequeueExisting(id, blobID int, contentHash, workspaceDir string) error {
	for _, job := range s.created {
		if job.ID == id {
			job.Status = MaterializeJobStatusPending
			job.BlobID = blobID
			job.ContentHash = contentHash
			job.WorkspaceDir = workspaceDir
			job.LastError = nil
		}
	}
	if s.latest != nil && s.latest.ID == id {
		s.latest.Status = MaterializeJobStatusPending
		s.latest.BlobID = blobID
		s.latest.ContentHash = contentHash
		s.latest.WorkspaceDir = workspaceDir
		s.latest.LastError = nil
	}
	return nil
}
func (s *materializeJobRepoStub) FindLatestBySkillID(int) (*models.SkillPackageMaterializeJob, error) {
	return s.latest, nil
}
func (s *materializeJobRepoStub) CountPendingByInstance(int) (int, error) { return 0, nil }
func (s *materializeJobRepoStub) ListBackfillCandidates(int) ([]repository.SkillPackageMaterializeBackfillCandidate, error) {
	return nil, nil
}

type materializeBlobRepoStub struct {
	blobs map[int]*models.SkillBlob
}

func (s *materializeBlobRepoStub) GetBlobByID(id int) (*models.SkillBlob, error) {
	if s.blobs == nil {
		return nil, nil
	}
	return s.blobs[id], nil
}

type testSkillScanner struct{}

func (testSkillScanner) ScanArchive(context.Context, string, []byte, map[string]string) (string, map[string]interface{}, string, error) {
	return skillRiskNone, map[string]interface{}{}, "ok", nil
}
func (testSkillScanner) AvailableAnalyzers(context.Context) ([]string, error) { return nil, nil }

func TestProcessJobMaterializesFromWorkspaceWithStorage(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "hermes", "user-1", "instance-1")
	skillRoot := filepath.Join(workspace, "home", ".hermes", "skills", "demo")
	if err := os.MkdirAll(filepath.Join(skillRoot, "src"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# demo\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	instance := &models.Instance{
		ID:            1,
		UserID:        1,
		Type:          RuntimeTypeHermes,
		InstanceMode:  InstanceModeLite,
		RuntimeType:   RuntimeBackendGateway,
		WorkspacePath: strPtr(workspace),
	}
	_, contentHash, err := loadLiteSkillDirectoryFromWorkspace(instance, "demo")
	if err != nil {
		t.Fatalf("loadLiteSkillDirectoryFromWorkspace() error = %v", err)
	}

	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive,
				SourceType: skillSourceDiscovered, CurrentVersionID: intPtr(10),
			},
		},
		versions: map[int]*models.SkillVersion{10: {ID: 10, SkillID: 1, BlobID: blobID}},
		blobs: map[int]*models.SkillBlob{
			blobID: {ID: blobID, ContentHash: contentHash, ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: instance}}
	storage := &importTestObjectStorage{objects: map[string][]byte{}}
	svc := &skillService{
		repo:         stub,
		instanceRepo: instRepo,
		storage:      storage,
		scanner:      testSkillScanner{},
	}
	job := &models.SkillPackageMaterializeJob{
		ID: 1, InstanceID: 1, SkillID: 1, BlobID: blobID,
		WorkspaceDir: "demo", ContentHash: contentHash, Status: MaterializeJobStatusPending,
	}
	jobRepo := &materializeJobRepoStub{latest: job, created: []*models.SkillPackageMaterializeJob{job}}
	matSvc := NewSkillPackageMaterializeService(jobRepo, stub, SkillServiceAsMaterializer(svc))

	if err := matSvc.ProcessJob(context.Background(), job.ID); err != nil {
		t.Fatalf("ProcessJob() error = %v", err)
	}
	if job.Status != MaterializeJobStatusSucceeded {
		t.Fatalf("job status = %q, want %q", job.Status, MaterializeJobStatusSucceeded)
	}
	updated, err := stub.GetBlobByID(blobID)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || strings.TrimSpace(updated.ObjectKey) == "" {
		t.Fatalf("expected blob object key, got %#v", updated)
	}
	if !strings.EqualFold(strings.TrimSpace(updated.ScanStatus), "completed") {
		t.Fatalf("blob scan_status = %q, want completed", updated.ScanStatus)
	}
	if _, ok := storage.objects[updated.ObjectKey]; !ok {
		t.Fatalf("storage missing object %q", updated.ObjectKey)
	}
}
