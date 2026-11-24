package systembackuptrial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	domain "clawreef/internal/systembackuptrial/records"
)

func TestTrialWorkspaceDirectoryScopeAndPagination(t *testing.T) {
	scope := workspaceSource{RelativeRoot: "home/projects"}
	for dir, expected := range map[string]string{"home/projects": ".", "home/projects/folder": "folder"} {
		if actual, ok := scope.mountedPath(dir); !ok || actual != expected {
			t.Fatalf("valid directory %q was not resolved", dir)
		}
	}
	for _, dir := range []string{"home", "home/projects-old/a", "home/projects/../private", "home/projects/.hidden", "/home/projects"} {
		if _, ok := scope.mountedPath(dir); ok {
			t.Fatalf("out-of-scope directory %q was accepted", dir)
		}
	}
	path := t.TempDir()
	for i := 0; i < workspaceDirectoryPageSize+2; i++ {
		if err := os.Mkdir(filepath.Join(path, fmt.Sprintf("folder-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(path, ".hidden"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "ordinary.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if err := os.Symlink(t.TempDir(), filepath.Join(path, "link")); err != nil {
			t.Fatal(err)
		}
	}
	mount, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer mount.Close()
	ctx := context.Background()
	first, err := readWorkspaceDirectories(ctx, mount, ".", scope.RelativeRoot, "")
	if err != nil || len(first.Directories) != 100 || first.Directories[0] != "folder-000" || first.Next != "folder-099" {
		t.Fatalf("unexpected first page: %+v, %v", first, err)
	}
	second, err := readWorkspaceDirectories(ctx, mount, ".", scope.RelativeRoot, first.Next)
	if err != nil || second.Next != "" || !slices.Equal(second.Directories, []string{"folder-100", "folder-101"}) {
		t.Fatalf("unexpected second page: %+v, %v", second, err)
	}
}

func TestTrialWorkspaceRegisteredReadOnlyRoundTrip(t *testing.T) {
	readonly := os.Getenv("CLAWMANAGER_WORKSPACE_TRIAL_SOURCE_MOUNT")
	if runtime.GOOS != "linux" || readonly == "" {
		t.Skip("requires the disposable read-only workspace fixture mount")
	}
	ctx := context.Background()
	_, profile, profileFile := trialEnvironmentFixture(t)
	profile.Side = "source"
	profile.Source.WorkspaceClaim = "source-workspace"
	profile.Source.WorkspaceFiles = &workspaceSource{UserID: 1, InstanceID: 7, Runtime: "hermes", RelativeRoot: "project"}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{"ENVIRONMENT_PROFILE_FILE": profileFile, "WORKSPACE_FILES_SELECTION_ROOT": readonly}
	service := New(func(key string) string { return settings[strings.TrimPrefix(key, "SYSTEM_BACKUP_TRIAL_")] })
	selection := workspaceSelection{UserID: 1, InstanceID: 7, RelativeDir: "project/sub/nested/selection"}
	archive, manifest, err := service.captureRegisteredWorkspace(ctx, historySQLFixture(), selection)
	if err != nil || manifest == nil || manifest.workspaceSelection != selection || bytes.Contains(archive, []byte("excluded workspace bytes")) {
		t.Fatalf("scoped workspace capture failed: %+v %v", manifest, err)
	}
	defer clear(archive)
	for _, invalid := range []workspaceSelection{{UserID: 2, InstanceID: 7, RelativeDir: selection.RelativeDir},
		{UserID: 1, InstanceID: 7, RelativeDir: "project-copy/sub"}} {
		if _, _, err := service.captureRegisteredWorkspace(ctx, historySQLFixture(), invalid); !errors.Is(err, domain.ErrTrialScope) {
			t.Fatalf("invalid selection returned %v", err)
		}
	}
	settings["WORKSPACE_FILES_SELECTION_ROOT"] = t.TempDir()
	if _, _, err := service.captureRegisteredWorkspace(ctx, historySQLFixture(), selection); !errors.Is(err, errWorkspaceFiles) {
		t.Fatalf("writable fixture mount returned %v", err)
	}
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := checkWorkspaceDestination(ctx, root, *manifest, archive); !errors.Is(err, errWorkspaceInstanceMissing) {
		t.Fatalf("missing instance root returned %v", err)
	}
	if err := root.MkdirAll(manifest.instanceLocation(), 0750); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkspaceDestination(ctx, root, *manifest, archive); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(manifest.instanceLocation() + "/project"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preflight created a final workspace path")
	}
	if err := restoreWorkspaceFiles(ctx, root, *manifest, archive); err != nil {
		t.Fatal(err)
	}
	if err := restoreWorkspaceFiles(ctx, root, *manifest, archive); err != nil {
		t.Fatalf("exact replay failed: %v", err)
	}
	raw, err := root.ReadFile(manifest.location() + "/uploads/input.txt")
	if err != nil || string(raw) != "selected workspace bytes" {
		t.Fatalf("selected fixture content did not survive restore: %v", err)
	}
}
