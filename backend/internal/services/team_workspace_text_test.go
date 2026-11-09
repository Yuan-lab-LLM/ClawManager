package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failedTeamTextAudit struct{}

func (failedTeamTextAudit) Create(context.Context, *models.TeamWorkspaceFileAudit) error {
	return errors.New("audit unavailable")
}

func TestTeamWorkspaceTextFailedAuditAndCancelKeepOriginal(t *testing.T) {
	svc := NewTeamService(&teamTextOwnerRepo{}, nil, WithTeamRuntimeWorkspaceRoot(t.TempDir()), WithTeamWorkspaceAuditRepository(failedTeamTextAudit{})).(*teamService)
	root := svc.teamRuntimeSharedPathFor(9, 17)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "a.md")
	if err := os.WriteFile(file, []byte("base"), 0640); err != nil {
		t.Fatal(err)
	}
	doc, err := svc.ReadWorkspaceText(context.Background(), 9, 17, "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveWorkspaceText(context.Background(), 9, 17, "a.md", "new", doc.Version); err == nil {
		t.Fatal("unrecorded write allowed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = svc.SaveWorkspaceText(ctx, 9, 17, "a.md", "new", doc.Version); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(file)
	if err != nil || string(bytes) != "base" {
		t.Fatal(string(bytes), err)
	}
	entries, err := svc.ListWorkspaceFiles(context.Background(), 9, 17, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries.Entries {
		if isWorkspaceTransientEntry(entry.Name) {
			t.Fatal("internal temporary/lock file exposed", entry)
		}
	}
}

type teamTextOwnerRepo struct{ repository.TeamRepository }

func (*teamTextOwnerRepo) GetTeamByID(id int) (*models.Team, error) {
	if id != 17 {
		return nil, nil
	}
	return &models.Team{ID: 17, UserID: 9}, nil
}

type teamTextCapability interface {
	ReadWorkspaceText(context.Context, int, int, string) (*WorkspaceText, error)
	SaveWorkspaceText(context.Context, int, int, string, string, string) (*WorkspaceText, error)
}

func TestTeamWorkspaceTextOwnerScopeAndConflict(t *testing.T) {
	ctx := context.Background()
	svc := NewTeamService(&teamTextOwnerRepo{}, nil, WithTeamRuntimeWorkspaceRoot(t.TempDir())).(*teamService)
	editor, ok := any(svc).(teamTextCapability)
	if !ok {
		t.Fatal("Team shared workspace has no text editing capability")
	}
	root := svc.teamRuntimeSharedPathFor(9, 17)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	original := "\ufeff# 共享产物\r\n保留原格式\r\n"
	if err := os.WriteFile(filepath.Join(root, "report.md"), []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	doc, err := editor.ReadWorkspaceText(ctx, 9, 17, "/team/report.md")
	if err != nil || doc.Text != original {
		t.Fatal(doc, err)
	}
	if _, err = editor.ReadWorkspaceText(ctx, 8, 17, "report.md"); err == nil {
		t.Fatal("foreign owner allowed")
	}
	for _, p := range []string{"../report.md", "/etc/report.md", "a/../../report.md", "/team/../report.md", "report.md\x00"} {
		if _, err = editor.ReadWorkspaceText(ctx, 9, 17, p); err == nil {
			t.Fatalf("unsafe path allowed: %q", p)
		}
	}
	next, err := editor.SaveWorkspaceText(ctx, 9, 17, "report.md", original+"new\r\n", doc.Version)
	if err != nil || next.Version == doc.Version {
		t.Fatal(next, err)
	}
	if _, err = editor.SaveWorkspaceText(ctx, 9, 17, "report.md", "lost", doc.Version); !errors.Is(err, ErrWorkspaceTextConflict) {
		t.Fatal("stale overwrite", err)
	}
	// Runtime writers are independent: a change since opening must not be replaced.
	if err = os.WriteFile(filepath.Join(root, "report.md"), []byte("worker output"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err = editor.SaveWorkspaceText(ctx, 9, 17, "report.md", "draft", next.Version); !errors.Is(err, ErrWorkspaceTextConflict) {
		t.Fatal("worker overwrite", err)
	}
	if _, err = editor.SaveWorkspaceText(ctx, 9, 17, "report.md", string(make([]byte, 1<<20+1)), next.Version); !errors.Is(err, ErrWorkspacePreviewTooLarge) {
		t.Fatal(err)
	}
	if _, err = editor.ReadWorkspaceText(ctx, 9, 17, "missing.md"); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
