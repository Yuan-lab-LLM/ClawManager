package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestMaterializeSelfHealsStaleBlobContentHash(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "hermes", "user-1", "instance-1")
	skillRoot := filepath.Join(workspace, "home", ".hermes", "skills", "demo")
	if err := os.MkdirAll(skillRoot, 0o750); err != nil {
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
	staleHash := "deadbeefdeadbeefdeadbeefdeadbeef"

	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {ID: 1, UserID: 1, SkillKey: "demo", Name: "Demo", Status: skillStatusActive, CurrentVersionID: intPtr(10)},
		},
		versions: map[int]*models.SkillVersion{10: {ID: 10, SkillID: 1, BlobID: blobID}},
		blobs: map[int]*models.SkillBlob{
			blobID: {ID: blobID, ContentHash: staleHash, ScanStatus: "pending", RiskLevel: skillRiskUnknown, ObjectKey: ""},
		},
	}
	instRepo := &importTestInstanceRepo{instances: map[int]*models.Instance{1: instance}}
	storage := &importTestObjectStorage{objects: map[string][]byte{}}
	svc := &skillService{repo: stub, instanceRepo: instRepo, storage: storage, scanner: testSkillScanner{}}

	blob, err := svc.materializeSkillPackageFromWorkspace(context.Background(), 1, "demo", staleHash, blobID)
	if err != nil {
		t.Fatalf("materializeSkillPackageFromWorkspace() error = %v", err)
	}
	if blob == nil || strings.TrimSpace(blob.ObjectKey) == "" {
		t.Fatalf("expected materialized blob, got %#v", blob)
	}
	if !strings.EqualFold(strings.TrimSpace(stub.blobs[blobID].ContentHash), contentHash) {
		t.Fatalf("blob content_hash = %q, want %q", stub.blobs[blobID].ContentHash, contentHash)
	}
}

func TestWorkspaceContentHashForLiteRecordOverridesAgentMD5(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "hermes", "user-1", "instance-1")
	skillRoot := filepath.Join(workspace, "home", ".hermes", "skills", "yuanbao")
	if err := os.MkdirAll(skillRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# yuanbao\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	instance := &models.Instance{
		Type: RuntimeTypeHermes, InstanceMode: InstanceModeLite,
		RuntimeType: RuntimeBackendGateway, WorkspacePath: strPtr(workspace),
	}
	got := workspaceContentHashForLiteRecord(instance, AgentSkillRecord{
		Identifier: "yuanbao", ContentMD5: "deadbeefdeadbeefdeadbeefdeadbeef",
	})
	_, want, err := loadLiteSkillDirectoryFromWorkspace(instance, "yuanbao")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspaceContentHashForLiteRecord() = %q, want %q", got, want)
	}
}

func TestResolveLiteWorkspaceDir(t *testing.T) {
	workspace := "yuanbao"
	instanceSkill := &models.InstanceSkill{
		WorkspaceDir: &workspace,
		InstallPath:  strPtr("home/.hermes/skills/yuanbao"),
	}
	skill := &models.Skill{SkillKey: "yuanbao-deadbeef", Name: "yuanbao"}
	if got := resolveLiteWorkspaceDir(instanceSkill, skill); got != "yuanbao" {
		t.Fatalf("resolveLiteWorkspaceDir() = %q, want yuanbao", got)
	}
}

func TestLoadLiteSkillDirectoryFromWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "hermes", "user-1", "instance-1")
	skillRoot := filepath.Join(workspace, "home", ".hermes", "skills", "weather")
	if err := os.MkdirAll(filepath.Join(skillRoot, "src"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# weather\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "src", "main.py"), []byte("print('ok')\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	instance := &models.Instance{
		Type:          RuntimeTypeHermes,
		InstanceMode:  InstanceModeLite,
		RuntimeType:   RuntimeBackendGateway,
		WorkspacePath: strPtr(workspace),
	}
	dir, md5, err := loadLiteSkillDirectoryFromWorkspace(instance, "weather")
	if err != nil {
		t.Fatalf("loadLiteSkillDirectoryFromWorkspace() error = %v", err)
	}
	if dir.Name != "weather" || len(dir.Files) != 2 {
		t.Fatalf("unexpected dir: %#v", dir)
	}
	if md5 == "" {
		t.Fatal("expected non-empty md5")
	}
}

func strPtr(value string) *string {
	return &value
}
