package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawreef/internal/models"
)

type recordingWorkspaceFileAuditRepo struct {
	items []models.WorkspaceFileAudit
	err   error
}

func (r *recordingWorkspaceFileAuditRepo) Create(ctx context.Context, audit *models.WorkspaceFileAudit) error {
	if r.err != nil {
		return r.err
	}
	r.items = append(r.items, *audit)
	return nil
}

func workspaceTestScope(root string) WorkspaceFileScope {
	return WorkspaceFileScope{
		InstanceID:    12,
		UserID:        34,
		WorkspacePath: root,
	}
}

func TestWorkspaceFileServiceListSortsDirectoriesFirstAndMarksCapabilities(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.log"), []byte("hello"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{0, 1, 2}, 0640); err != nil {
		t.Fatal(err)
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	entries, err := service.List(context.Background(), workspaceTestScope(root), "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	if strings.Join(names, ",") != "docs,alpha.log,binary.dat" {
		t.Fatalf("entry order = %v, want dirs first then names", names)
	}
	if entries[1].Path != "alpha.log" || !entries[1].Previewable || !entries[1].Downloadable {
		t.Fatalf("alpha.log capabilities = %#v, want previewable downloadable file", entries[1])
	}
	if entries[2].Previewable || !entries[2].Downloadable {
		t.Fatalf("binary.dat capabilities = %#v, want download-only file", entries[2])
	}
}

func TestWorkspaceFileServiceListHidesTransientSkillTempDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".tmp"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".tmp-skill-paper-ranker-123"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "paper-ranker"), 0750); err != nil {
		t.Fatal(err)
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	entries, err := service.List(context.Background(), workspaceTestScope(root), "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if len(entries) != 1 || entries[0].Name != "paper-ranker" {
		t.Fatalf("entries = %#v, want only paper-ranker", entries)
	}
}
func TestWorkspaceFileServicePreviewTextAndDoesNotAudit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# hello"), 0640); err != nil {
		t.Fatal(err)
	}
	auditRepo := &recordingWorkspaceFileAuditRepo{}

	service := NewWorkspaceFileService(auditRepo)
	preview, err := service.Preview(context.Background(), workspaceTestScope(root), "notes.md")
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}

	if preview.Kind != "text" || preview.Text != "# hello" {
		t.Fatalf("preview = %#v, want text preview", preview)
	}
	if len(auditRepo.items) != 0 {
		t.Fatalf("preview audited %d events, want none", len(auditRepo.items))
	}
}

func TestWorkspaceFileServiceTreatsSVGAsDownloadOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "icon.svg"), []byte(`<svg onload="alert(1)"></svg>`), 0640); err != nil {
		t.Fatal(err)
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	entries, err := service.List(context.Background(), workspaceTestScope(root), "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Previewable {
		t.Fatalf("svg entry = %#v, want download-only", entries)
	}
	preview, err := service.Preview(context.Background(), workspaceTestScope(root), "icon.svg")
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}
	if preview.Kind != "binary" || preview.PreviewURL != "" {
		t.Fatalf("svg preview = %#v, want binary without preview url", preview)
	}
}

func TestWorkspaceFileServiceRejectsLargeTextPreview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.log"), []byte(strings.Repeat("a", 1024*1024+1)), 0640); err != nil {
		t.Fatal(err)
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	_, err := service.Preview(context.Background(), workspaceTestScope(root), "large.log")
	if !errors.Is(err, ErrWorkspacePreviewTooLarge) {
		t.Fatalf("Preview error = %v, want ErrWorkspacePreviewTooLarge", err)
	}
}

func TestWorkspaceFileServiceDownloadAuditsSuccessfulFileOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("report"), 0640); err != nil {
		t.Fatal(err)
	}
	auditRepo := &recordingWorkspaceFileAuditRepo{}

	service := NewWorkspaceFileService(auditRepo)
	file, filename, size, err := service.OpenDownload(context.Background(), workspaceTestScope(root), "report.txt")
	if err != nil {
		t.Fatalf("OpenDownload returned error: %v", err)
	}
	file.Close()

	if filename != "report.txt" || size != int64(len("report")) {
		t.Fatalf("download metadata filename=%q size=%d", filename, size)
	}
	if len(auditRepo.items) != 1 || auditRepo.items[0].Action != "download" || auditRepo.items[0].RelativePath != "report.txt" {
		t.Fatalf("audit items = %#v, want download audit", auditRepo.items)
	}
}

func TestWorkspaceFileServiceUploadRejectsEscapingSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink creation is not available: %v", err)
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	_, err := service.Upload(context.Background(), workspaceTestScope(root), "linked", "owned.txt", strings.NewReader("owned"), int64(len("owned")))
	if !errors.Is(err, ErrWorkspacePathEscape) {
		t.Fatalf("Upload error = %v, want ErrWorkspacePathEscape", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "owned.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside file stat error = %v, want not exist", statErr)
	}
}

func TestWorkspaceFileServiceUploadEnforcesMaxBytesBeforeWriting(t *testing.T) {
	t.Setenv(WorkspaceUploadMaxBytesEnv, "3")
	root := t.TempDir()

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	_, err := service.Upload(context.Background(), workspaceTestScope(root), "", "big.txt", strings.NewReader("abcd"), int64(len("abcd")))
	if !errors.Is(err, ErrWorkspaceUploadTooLarge) {
		t.Fatalf("Upload error = %v, want ErrWorkspaceUploadTooLarge", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "big.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("uploaded file stat error = %v, want not exist", statErr)
	}
}

func TestWorkspaceFileServiceRuntimeWorkspaceOwner(t *testing.T) {
	scope := WorkspaceFileScope{
		InstanceID:    74,
		UserID:        1,
		WorkspacePath: "/workspaces/openclaw/user-1/instance-74",
	}
	uid, gid, ok := runtimeWorkspaceOwner(scope)
	if !ok {
		t.Fatal("runtimeWorkspaceOwner ok = false, want true")
	}
	if uid != RuntimeLinuxID(74) || gid != RuntimeLinuxID(74) {
		t.Fatalf("runtime owner uid:gid = %d:%d, want %d:%d", uid, gid, RuntimeLinuxID(74), RuntimeLinuxID(74))
	}

	scope.WorkspacePath = "/tmp/workspaces/openclaw/user-1/instance-74"
	if _, _, ok := runtimeWorkspaceOwner(scope); !ok {
		t.Fatal("runtimeWorkspaceOwner should support configurable workspace roots")
	}

	scope.WorkspacePath = "/workspaces/openclaw/user-2/instance-74"
	if _, _, ok := runtimeWorkspaceOwner(scope); ok {
		t.Fatal("runtimeWorkspaceOwner ok = true for mismatched user")
	}

	scope.WorkspacePath = "/var/lib/clawmanager/user-1/instance-74"
	if _, _, ok := runtimeWorkspaceOwner(scope); ok {
		t.Fatal("runtimeWorkspaceOwner ok = true for non-runtime workspace")
	}
}

func TestWorkspaceFileServiceUploadAuditFailureRemovesFile(t *testing.T) {
	root := t.TempDir()
	auditRepo := &recordingWorkspaceFileAuditRepo{err: errors.New("audit unavailable")}
	service := NewWorkspaceFileService(auditRepo)

	_, err := service.Upload(context.Background(), workspaceTestScope(root), "", "created.txt", strings.NewReader("body"), int64(len("body")))
	if err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("Upload error = %v, want audit failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "created.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created file stat error = %v, want not exist", statErr)
	}
}

func TestWorkspaceFileServiceMkdirAuditFailureRemovesDirectory(t *testing.T) {
	root := t.TempDir()
	auditRepo := &recordingWorkspaceFileAuditRepo{err: errors.New("audit unavailable")}
	service := NewWorkspaceFileService(auditRepo)

	_, err := service.Mkdir(context.Background(), workspaceTestScope(root), "docs")
	if err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("Mkdir error = %v, want audit failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "docs")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created directory stat error = %v, want not exist", statErr)
	}
}

func TestWorkspaceFileServiceRenameAuditFailureRollsBack(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("body"), 0640); err != nil {
		t.Fatal(err)
	}
	auditRepo := &recordingWorkspaceFileAuditRepo{err: errors.New("audit unavailable")}
	service := NewWorkspaceFileService(auditRepo)

	_, err := service.Rename(context.Background(), workspaceTestScope(root), "old.txt", "new.txt")
	if err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("Rename error = %v, want audit failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "old.txt")); statErr != nil {
		t.Fatalf("old file stat error = %v, want rollback", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "new.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("new file stat error = %v, want not exist", statErr)
	}
}

func TestWorkspaceFileServiceDeleteAuditFailureKeepsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("body"), 0640); err != nil {
		t.Fatal(err)
	}
	auditRepo := &recordingWorkspaceFileAuditRepo{err: errors.New("audit unavailable")}
	service := NewWorkspaceFileService(auditRepo)

	err := service.Delete(context.Background(), workspaceTestScope(root), "keep.txt")
	if err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("Delete error = %v, want audit failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "keep.txt")); statErr != nil {
		t.Fatalf("kept file stat error = %v", statErr)
	}
}

func TestWorkspaceFileServiceMutationsAuditAndProtectRoot(t *testing.T) {
	root := t.TempDir()
	auditRepo := &recordingWorkspaceFileAuditRepo{}
	service := NewWorkspaceFileService(auditRepo)
	scope := workspaceTestScope(root)

	if err := service.Delete(context.Background(), scope, ""); !errors.Is(err, ErrWorkspaceRootOperation) {
		t.Fatalf("Delete root error = %v, want ErrWorkspaceRootOperation", err)
	}
	if _, err := service.Rename(context.Background(), scope, "", "renamed"); !errors.Is(err, ErrWorkspaceRootOperation) {
		t.Fatalf("Rename root error = %v, want ErrWorkspaceRootOperation", err)
	}
	if _, err := service.Mkdir(context.Background(), scope, "docs"); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	if _, err := service.Rename(context.Background(), scope, "docs", "notes"); err != nil {
		t.Fatalf("Rename returned error: %v", err)
	}
	if err := service.Delete(context.Background(), scope, "notes"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	actions := make([]string, 0, len(auditRepo.items))
	for _, item := range auditRepo.items {
		actions = append(actions, item.Action)
	}
	if strings.Join(actions, ",") != "mkdir,rename,delete" {
		t.Fatalf("audit actions = %v, want mkdir,rename,delete", actions)
	}
}
