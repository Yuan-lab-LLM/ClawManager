package services

import (
	"testing"

	"clawreef/internal/models"
)

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
