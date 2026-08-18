package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureOpenClawPluginLayoutCompatibilityLeavesLegacyLayoutUntouched(t *testing.T) {
	workspace := t.TempDir()
	globalRoot := filepath.Join(workspace, "home", ".openclaw", "npm", "node_modules")
	defaultsPackage := filepath.Join(workspace, "defaults", "legacy-plugin")
	if err := os.MkdirAll(defaultsPackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(globalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyLink := filepath.Join(globalRoot, "legacy-plugin")
	if err := os.Symlink(defaultsPackage, legacyLink); err != nil {
		t.Fatal(err)
	}

	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err != nil {
		t.Fatalf("ensureOpenClawPluginLayoutCompatibility returned error: %v", err)
	}

	info, err := os.Lstat(legacyLink)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy plugin link changed: info=%v err=%v", info, err)
	}
}

func TestEnsureOpenClawPluginLayoutCompatibilityLinksProjectPackages(t *testing.T) {
	workspace := t.TempDir()
	npmRoot := filepath.Join(workspace, "home", ".openclaw", "npm")
	projectModules := filepath.Join(npmRoot, "projects", "openclaw-feishu", "node_modules")
	feishuPackage := filepath.Join(projectModules, "@openclaw", "feishu")
	dingtalkPackage := filepath.Join(projectModules, "dingtalk-connector")
	for _, dir := range []string{feishuPackage, dingtalkPackage} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "openclaw.plugin.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	globalRoot := filepath.Join(npmRoot, "node_modules")
	legacyPackage := filepath.Join(globalRoot, "dingtalk-connector")
	if err := os.MkdirAll(legacyPackage, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err != nil {
		t.Fatalf("ensureOpenClawPluginLayoutCompatibility returned error: %v", err)
	}

	feishuLink := filepath.Join(globalRoot, "@openclaw", "feishu")
	info, err := os.Lstat(feishuLink)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("scoped compatibility link missing: info=%v err=%v", info, err)
	}
	resolved, err := filepath.EvalSymlinks(feishuLink)
	if err != nil {
		t.Fatal(err)
	}
	wantResolved, err := filepath.EvalSymlinks(feishuPackage)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wantResolved {
		t.Fatalf("compatibility link resolves to %q, want %q", resolved, wantResolved)
	}
	legacyInfo, err := os.Lstat(legacyPackage)
	if err != nil || !legacyInfo.IsDir() || legacyInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("existing legacy package was replaced: info=%v err=%v", legacyInfo, err)
	}

	// The operation is intentionally idempotent for retries and scheduler loops.
	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err != nil {
		t.Fatalf("second compatibility pass returned error: %v", err)
	}
}

func TestEnsureOpenClawPluginLayoutCompatibilityRejectsAmbiguousPackage(t *testing.T) {
	workspace := t.TempDir()
	projectsRoot := filepath.Join(workspace, "home", ".openclaw", "npm", "projects")
	for _, project := range []string{"project-a", "project-b"} {
		packagePath := filepath.Join(projectsRoot, project, "node_modules", "same-plugin")
		if err := os.MkdirAll(packagePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packagePath, "openclaw.plugin.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0)
	if err == nil {
		t.Fatal("expected ambiguous plugin error")
	}
}

func TestEnsureOpenClawPluginLayoutCompatibilityIgnoresDuplicateTransitiveDependencies(t *testing.T) {
	workspace := t.TempDir()
	projectsRoot := filepath.Join(workspace, "home", ".openclaw", "npm", "projects")
	for _, project := range []string{"plugin-a", "plugin-b"} {
		dependencyPath := filepath.Join(projectsRoot, project, "node_modules", "asynckit")
		if err := os.MkdirAll(dependencyPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dependencyPath, "package.json"), []byte(`{"name":"asynckit"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err != nil {
		t.Fatalf("duplicate transitive dependency blocked gateway preparation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "home", ".openclaw", "npm", "node_modules", "asynckit")); !os.IsNotExist(err) {
		t.Fatalf("transitive dependency received a legacy plugin link: %v", err)
	}
}

func TestQuarantineCorruptLegacyOpenClawTaskStateMovesRecoverableDatabaseSet(t *testing.T) {
	workspace := t.TempDir()
	tasksRoot := filepath.Join(workspace, "home", ".openclaw", "tasks")
	if err := os.MkdirAll(tasksRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range []string{"runs.sqlite", "runs.sqlite-wal", "runs.sqlite-shm"} {
		if err := os.WriteFile(filepath.Join(tasksRoot, fileName), []byte(fileName), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	errorMessage := "startup migration failed: database disk image is malformed"

	moved, err := quarantineCorruptLegacyOpenClawTaskState(workspace, 34, &errorMessage)
	if err != nil {
		t.Fatalf("quarantineCorruptLegacyOpenClawTaskState returned error: %v", err)
	}
	if !moved {
		t.Fatal("expected corrupt legacy task database to be quarantined")
	}
	quarantineRoot := filepath.Join(workspace, "home", ".openclaw", "quarantine")
	entries, err := os.ReadDir(quarantineRoot)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "legacy-tasks-generation-34") {
		t.Fatalf("quarantine entries = %#v, err=%v", entries, err)
	}
	for _, fileName := range []string{"runs.sqlite", "runs.sqlite-wal", "runs.sqlite-shm"} {
		if _, err := os.Stat(filepath.Join(tasksRoot, fileName)); !os.IsNotExist(err) {
			t.Fatalf("legacy file %q still exists: %v", fileName, err)
		}
		if _, err := os.Stat(filepath.Join(quarantineRoot, entries[0].Name(), fileName)); err != nil {
			t.Fatalf("quarantined file %q missing: %v", fileName, err)
		}
	}
}

func TestQuarantineCorruptLegacyOpenClawTaskStateIgnoresNonCorruptionFailure(t *testing.T) {
	workspace := t.TempDir()
	tasksRoot := filepath.Join(workspace, "home", ".openclaw", "tasks")
	if err := os.MkdirAll(tasksRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(tasksRoot, "runs.sqlite")
	if err := os.WriteFile(databasePath, []byte("healthy-or-unknown"), 0o600); err != nil {
		t.Fatal(err)
	}
	errorMessage := "gateway health check timed out"

	moved, err := quarantineCorruptLegacyOpenClawTaskState(workspace, 7, &errorMessage)
	if err != nil || moved {
		t.Fatalf("non-corruption result = moved %v, err %v", moved, err)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("non-corrupt database changed: %v", err)
	}
}
