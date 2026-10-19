package services

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
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
	if !entries[0].IsDir || !entries[0].Downloadable {
		t.Fatalf("docs capabilities = %#v, want downloadable directory", entries[0])
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

func TestWorkspaceFileServiceSearchRecursivelyMatchesNames(t *testing.T) {
	root := t.TempDir()
	layout := []struct {
		relative string
		dir      bool
	}{
		{relative: "docs", dir: true},
		{relative: filepath.Join("docs", "README.md")},
		{relative: filepath.Join("docs", "deep", "notes.txt"), dir: true},
		{relative: filepath.Join("docs", "deep", "notes.txt", "readme.log")},
		{relative: filepath.Join("node_modules", "left-pad", "readme.js"), dir: true},
		{relative: filepath.Join(".git", "readme"), dir: true},
		{relative: ".tmp", dir: true},
		{relative: ".tmp-skill-demo"},
	}
	for _, item := range layout {
		target := filepath.Join(root, item.relative)
		if item.dir {
			if err := os.MkdirAll(target, 0750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("x"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	entries, err := service.Search(context.Background(), workspaceTestScope(root), "READ", 0)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	want := strings.Join([]string{
		filepath.ToSlash(filepath.Join("docs", "deep", "notes.txt", "readme.log")),
		filepath.ToSlash(filepath.Join("docs", "README.md")),
	}, ",")
	if strings.Join(paths, ",") != want {
		t.Fatalf("search paths = %v, want %v", paths, want)
	}
	if entries[0].IsDir {
		t.Fatalf("entry %#v, want a file", entries[0])
	}
	if !entries[0].Previewable || !entries[0].Downloadable {
		t.Fatalf("entry capabilities = %#v, want previewable and downloadable", entries[0])
	}
}

func TestWorkspaceFileServiceSearchEmptyQueryReturnsNoEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0640); err != nil {
		t.Fatal(err)
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	entries, err := service.Search(context.Background(), workspaceTestScope(root), "   ", 0)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want none for empty query", entries)
	}
}

func TestWorkspaceFileServiceSearchHonorsLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a-readme.md", "b-readme.md", "c-readme.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	service := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{})
	entries, err := service.Search(context.Background(), workspaceTestScope(root), "readme", 2)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (explicit limit honored)", len(entries))
	}

	entries, err = service.Search(context.Background(), workspaceTestScope(root), "readme", 0)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 (default limit keeps every match)", len(entries))
	}
}

func TestNormalizeWorkspaceSearchLimit(t *testing.T) {
	cases := []struct {
		limit int
		want  int
	}{
		{limit: 0, want: WorkspaceSearchMaxResults},
		{limit: -5, want: WorkspaceSearchMaxResults},
		{limit: 50, want: 50},
		{limit: WorkspaceSearchLimitCeiling, want: WorkspaceSearchLimitCeiling},
		{limit: WorkspaceSearchLimitCeiling + 1, want: WorkspaceSearchLimitCeiling},
	}
	for _, testCase := range cases {
		if got := NormalizeWorkspaceSearchLimit(testCase.limit); got != testCase.want {
			t.Fatalf("NormalizeWorkspaceSearchLimit(%d) = %d, want %d", testCase.limit, got, testCase.want)
		}
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

func TestWorkspaceFileServiceDownloadsDirectoryAsZipAndCleansTemporaryFile(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "资料")
	if err := os.MkdirAll(filepath.Join(docs, "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "说明.txt"), []byte("hello"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(docs, ".tmp-skill-hidden"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, ".tmp-skill-hidden", "secret.txt"), []byte("hidden"), 0640); err != nil {
		t.Fatal(err)
	}

	auditRepo := &recordingWorkspaceFileAuditRepo{}
	service := NewWorkspaceFileService(auditRepo)
	download, filename, size, err := service.OpenDownload(context.Background(), workspaceTestScope(root), "资料")
	if err != nil {
		t.Fatalf("OpenDownload directory returned error: %v", err)
	}
	temporary, ok := download.(*workspaceTemporaryDownload)
	if !ok {
		t.Fatalf("directory download type = %T, want temporary download", download)
	}
	temporaryPath := temporary.path
	data, err := io.ReadAll(download)
	if err != nil {
		t.Fatalf("read directory archive: %v", err)
	}
	if err := download.Close(); err != nil {
		t.Fatalf("close directory archive: %v", err)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary archive remains after close: %v", err)
	}
	if filename != "资料.zip" || size != int64(len(data)) || size <= 0 {
		t.Fatalf("directory download metadata filename=%q size=%d data=%d", filename, size, len(data))
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open directory zip: %v", err)
	}
	names := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	joined := strings.Join(names, ",")
	for _, expected := range []string{"资料/", "资料/empty/", "资料/说明.txt"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("zip entries = %v, missing %q", names, expected)
		}
	}
	if strings.Contains(joined, ".tmp-skill-hidden") {
		t.Fatalf("zip entries include transient directory: %v", names)
	}
	if len(auditRepo.items) != 1 || auditRepo.items[0].Action != "download" || auditRepo.items[0].RelativePath != "资料" {
		t.Fatalf("audit items = %#v, want directory download audit", auditRepo.items)
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
