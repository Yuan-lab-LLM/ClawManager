package services

import (
	"context"
	"fmt"
	"testing"

	"clawreef/internal/models"
)

type failingSkillScanner struct{}

func (failingSkillScanner) ScanArchive(context.Context, string, []byte, map[string]string) (string, map[string]interface{}, string, error) {
	return "", nil, "", fmt.Errorf("scanner rejected archive")
}

func (failingSkillScanner) AvailableAnalyzers(context.Context) ([]string, error) { return nil, nil }

func TestPreviewImportDirectoryNone(t *testing.T) {
	svc := &skillService{repo: &skillRepoStub{skills: map[int]*models.Skill{}}}
	item, err := svc.previewImportDirectory(1, extractedSkillDirectory{
		Name:  "weather",
		Files: map[string][]byte{"SKILL.md": []byte("# weather")},
	})
	if err != nil {
		t.Fatalf("previewImportDirectory() error = %v", err)
	}
	if item.ConflictType != skillImportConflictNone {
		t.Fatalf("conflict_type = %q, want %q", item.ConflictType, skillImportConflictNone)
	}
}

func TestPreviewImportDirectoryAcceptsChineseName(t *testing.T) {
	svc := &skillService{repo: &skillRepoStub{skills: map[int]*models.Skill{}}}
	item, err := svc.previewImportDirectory(1, extractedSkillDirectory{
		Name:  "股票价值投资分析",
		Files: map[string][]byte{"SKILL.md": []byte("# 股票价值投资分析")},
	})
	if err != nil {
		t.Fatalf("previewImportDirectory() error = %v", err)
	}
	if item.SkillKey != "股票价值投资分析" {
		t.Fatalf("SkillKey = %q, want 股票价值投资分析", item.SkillKey)
	}
	if item.ConflictType != skillImportConflictNone {
		t.Fatalf("conflict_type = %q, want %q", item.ConflictType, skillImportConflictNone)
	}
}

func TestImportDirectoryWritesFrontmatterDescription(t *testing.T) {
	stub := &skillRepoStub{skills: map[int]*models.Skill{}, blobs: map[int]*models.SkillBlob{}, versions: map[int]*models.SkillVersion{}}
	svc := &skillService{repo: stub, storage: fakeObjectStorage{}, scanner: testSkillScanner{}}
	dir := extractedSkillDirectory{
		Name: "demo-skill",
		Files: map[string][]byte{
			"SKILL.md": []byte("---\nname: demo\ndescription: 从 frontmatter 读取\n---\n# Demo\n"),
		},
	}
	result, err := svc.importDirectoryWithOptions(context.Background(), 1, dir, "demo.zip", ImportDirectoryOptions{Action: skillImportActionAuto})
	if err != nil {
		t.Fatalf("importDirectoryWithOptions() error = %v", err)
	}
	if result.Skill.Description == nil || *result.Skill.Description != "从 frontmatter 读取" {
		t.Fatalf("Description = %#v, want frontmatter text", result.Skill.Description)
	}
}

func TestImportDirectoryWithoutDescriptionSucceeds(t *testing.T) {
	stub := &skillRepoStub{skills: map[int]*models.Skill{}, blobs: map[int]*models.SkillBlob{}, versions: map[int]*models.SkillVersion{}}
	svc := &skillService{repo: stub, storage: fakeObjectStorage{}, scanner: testSkillScanner{}}
	dir := extractedSkillDirectory{
		Name: "no-desc-skill",
		Files: map[string][]byte{
			"SKILL.md": []byte("---\nname: no-desc\n---\n# Body\n"),
		},
	}
	result, err := svc.importDirectoryWithOptions(context.Background(), 1, dir, "no-desc.zip", ImportDirectoryOptions{Action: skillImportActionAuto})
	if err != nil {
		t.Fatalf("importDirectoryWithOptions() error = %v", err)
	}
	if result.Skill.Description != nil {
		t.Fatalf("Description = %#v, want nil", result.Skill.Description)
	}
}

func TestImportDirectorySucceedsWhenScannerFails(t *testing.T) {
	stub := &skillRepoStub{skills: map[int]*models.Skill{}, blobs: map[int]*models.SkillBlob{}, versions: map[int]*models.SkillVersion{}}
	svc := &skillService{repo: stub, storage: fakeObjectStorage{}, scanner: failingSkillScanner{}}
	dir := extractedSkillDirectory{Name: "scanner-failure", Files: map[string][]byte{"SKILL.md": []byte("# Scanner failure")}}

	result, err := svc.importDirectoryWithOptions(context.Background(), 1, dir, "scanner-failure.zip", ImportDirectoryOptions{Action: skillImportActionAuto})
	if err != nil {
		t.Fatalf("importDirectoryWithOptions() error = %v", err)
	}
	if result.Skill.ScanStatus != "failed" {
		t.Fatalf("ScanStatus = %q, want failed", result.Skill.ScanStatus)
	}
	if result.Skill.RiskLevel != skillRiskUnknown {
		t.Fatalf("RiskLevel = %q, want %q", result.Skill.RiskLevel, skillRiskUnknown)
	}
	if blob := stub.blobs[1]; blob == nil || blob.ScanStatus != "failed" {
		t.Fatalf("blob scan status = %#v, want failed", blob)
	}
}

func TestPreviewImportDirectoryUnchanged(t *testing.T) {
	versionID := 10
	blobID := 20
	dir := extractedSkillDirectory{Name: "weather", Files: map[string][]byte{"SKILL.md": []byte("# weather")}}
	contentHash := hashDirectory(dir.Files)
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "weather", Name: "Weather", Status: skillStatusActive,
				SourceType: skillSourceUploaded, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID, VersionNo: 2}},
		blobs:    map[int]*models.SkillBlob{blobID: {ID: blobID, ContentHash: contentHash, ScanStatus: "completed"}},
	}
	svc := &skillService{repo: stub}
	item, err := svc.previewImportDirectory(1, dir)
	if err != nil {
		t.Fatalf("previewImportDirectory() error = %v", err)
	}
	if item.ConflictType != skillImportConflictUnchanged {
		t.Fatalf("conflict_type = %q, want %q", item.ConflictType, skillImportConflictUnchanged)
	}
}

func TestPreviewImportDirectoryContentChanged(t *testing.T) {
	versionID := 10
	blobID := 20
	stub := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 1, SkillKey: "weather", Name: "Weather", Status: skillStatusActive,
				SourceType: skillSourceUploaded, CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{versionID: {ID: versionID, SkillID: 1, BlobID: blobID, VersionNo: 2}},
		blobs:    map[int]*models.SkillBlob{blobID: {ID: blobID, ContentHash: "old-hash", ScanStatus: "completed"}},
	}
	svc := &skillService{repo: stub}
	item, err := svc.previewImportDirectory(1, extractedSkillDirectory{
		Name:  "weather",
		Files: map[string][]byte{"SKILL.md": []byte("# changed")},
	})
	if err != nil {
		t.Fatalf("previewImportDirectory() error = %v", err)
	}
	if item.ConflictType != skillImportConflictContentChanged {
		t.Fatalf("conflict_type = %q, want %q", item.ConflictType, skillImportConflictContentChanged)
	}
	if item.SuggestedSkillKey == nil || *item.SuggestedSkillKey != "weather-2" {
		t.Fatalf("suggested skill key = %v, want weather-2", item.SuggestedSkillKey)
	}
}
