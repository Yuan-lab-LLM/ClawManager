package services

import (
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestGetSkillMarkdownSuccess(t *testing.T) {
	versionID := 11
	blobID := 21
	archive := buildTestZip(t, map[string][]byte{
		"demo/SKILL.md": []byte("---\nname: demo\ndescription: hello\n---\n# Demo Body\n"),
	})
	repo := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 7, SkillKey: "demo", Name: "demo", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate,
				CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{
			versionID: {ID: versionID, SkillID: 1, BlobID: blobID, VersionNo: 1},
		},
		blobs: map[int]*models.SkillBlob{
			blobID: {ID: blobID, ObjectKey: "obj/demo.zip", FileName: "demo.zip"},
		},
	}
	svc := &skillService{
		repo:    repo,
		storage: fakeObjectStorage{"obj/demo.zip": archive},
	}

	content, err := svc.GetSkillMarkdown(7, "user", 1)
	if err != nil {
		t.Fatalf("GetSkillMarkdown() error = %v", err)
	}
	if !strings.Contains(content, "description: hello") || !strings.Contains(content, "# Demo Body") {
		t.Fatalf("content = %q", content)
	}
}

func TestGetSkillMarkdownPackagePending(t *testing.T) {
	versionID := 11
	blobID := 21
	repo := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 7, SkillKey: "demo", Name: "demo", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate,
				CurrentVersionID: &versionID,
			},
		},
		versions: map[int]*models.SkillVersion{
			versionID: {ID: versionID, SkillID: 1, BlobID: blobID, VersionNo: 1},
		},
		blobs: map[int]*models.SkillBlob{
			blobID: {ID: blobID, ObjectKey: "", FileName: "demo.zip"},
		},
	}
	svc := &skillService{repo: repo, storage: fakeObjectStorage{}}

	_, err := svc.GetSkillMarkdown(7, "user", 1)
	if err == nil || !strings.Contains(err.Error(), "skill_package_pending") {
		t.Fatalf("error = %v, want skill_package_pending", err)
	}
}

func TestGetSkillMarkdownNotFound(t *testing.T) {
	versionID := 11
	repo := &skillRepoStub{
		skills: map[int]*models.Skill{
			1: {
				ID: 1, UserID: 7, SkillKey: "demo", Name: "demo", Status: skillStatusActive,
				SourceType: skillSourceUploaded, Visibility: skillVisibilityPrivate,
				CurrentVersionID: &versionID,
			},
		},
	}
	svc := &skillService{repo: repo, storage: fakeObjectStorage{}}

	_, err := svc.GetSkillMarkdown(99, "user", 1)
	if err == nil || !strings.Contains(err.Error(), "skill not found") {
		t.Fatalf("error = %v, want skill not found", err)
	}

	_, err = svc.GetSkillMarkdown(7, "user", 404)
	if err == nil || !strings.Contains(err.Error(), "skill not found") {
		t.Fatalf("missing skill error = %v, want skill not found", err)
	}
}
