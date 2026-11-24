package systembackuptrial

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func workspaceFixture(t *testing.T) ([]byte, workspaceManifest) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "hermes", "user-1", "instance-7", "project")
	if err := os.MkdirAll(filepath.Join(project, "uploads"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "uploads", "input.txt"), []byte("fixture input"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "report.bin"), []byte{0, 1, 255, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "..", "sibling.txt"), []byte("unselected fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	selection := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(selection, []byte(`{"user_id":1,"instance_id":7,"relative_dir":"project"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"SYSTEM_BACKUP_TRIAL_WORKSPACE_FILES_SELECTION_FILE": selection,
		"SYSTEM_BACKUP_TRIAL_WORKSPACE_FILES_SOURCE_ROOT":    root,
	}
	archive, m, err := New(func(name string) string { return env[name] }).captureWorkspaceFiles(context.Background(), historySQLFixture())
	if err != nil || m == nil || m.Entries != 4 || m.Bytes != 17 || bytes.Contains(archive, []byte("unselected fixture")) {
		t.Fatalf("selected workspace capture failed: %+v %v", m, err)
	}
	return archive, *m
}

func TestTrialWorkspaceBindsSelectionToOwnerAndSafeDirectory(t *testing.T) {
	snapshot := historySQLFixture()
	for _, selection := range []workspaceSelection{
		{UserID: 40, InstanceID: 7, RelativeDir: "project"},
		{UserID: 1, InstanceID: 77, RelativeDir: "project"},
		{UserID: 1, InstanceID: 7, RelativeDir: "home"},
		{UserID: 1, InstanceID: 7, RelativeDir: "home/.hermes"},
		{UserID: 1, InstanceID: 7, RelativeDir: "project/../other"},
	} {
		if got := selectedRuntime(snapshot, selection); got != "" {
			t.Fatalf("unsafe selection admitted: %+v", selection)
		}
	}
	if got := selectedRuntime(snapshot, workspaceSelection{UserID: 1, InstanceID: 7, RelativeDir: "project"}); got != "hermes" {
		t.Fatalf("valid workspace selection lost: %s", got)
	}
}

func TestTrialWorkspaceRestorePublishesVerifiedFilesWithoutOverwriting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux no-replace directory publication")
	}
	ctx := context.Background()
	archive, m := workspaceFixture(t)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			targetPath := t.TempDir()
			root, err := os.OpenRoot(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := root.MkdirAll("hermes/user-1/instance-7", 0750); err != nil {
				t.Fatal(err)
			}
			if err := checkWorkspaceDestination(ctx, root, m, archive); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(targetPath, "hermes/user-1/instance-7"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("workspace destination check left test entries: %v, %v", entries, err)
			}
			if err := restoreWorkspaceFiles(ctx, root, m, archive); err != nil {
				t.Fatal(err)
			}
			if err := verifyWorkspaceFiles(ctx, root, m, archive); err != nil {
				t.Fatal(err)
			}
			if err := restoreWorkspaceFiles(ctx, root, m, archive); err != nil {
				t.Fatalf("identical replay failed: %v", err)
			}
			file := m.location() + "/uploads/input.txt"
			if got, err := root.ReadFile(file); err != nil || string(got) != "fixture input" {
				t.Fatalf("expected input from original archive: %v", err)
			}
			if err := root.WriteFile(file, []byte("changed by target"), 0640); err != nil {
				t.Fatal(err)
			}
			if err := restoreWorkspaceFiles(ctx, root, m, archive); !errors.Is(err, errWorkspaceFiles) {
				t.Fatalf("non-matching target was accepted: %v", err)
			}
			if got, err := root.ReadFile(file); err != nil || string(got) != "changed by target" {
				t.Fatalf("rejected restore changed existing data: %v", err)
			}
		})
	}
}
