package services

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func skipIfTestSymlinkUnavailable(t *testing.T, err error) {
	t.Helper()
	if err != nil && runtime.GOOS == "windows" && strings.Contains(strings.ToLower(err.Error()), "required privilege") {
		t.Skipf("symlink assertions require Windows Developer Mode or SeCreateSymbolicLinkPrivilege: %v", err)
	}
}

func TestOpenClawCompatibilityDoesNotCreateAFirstStartWorkspace(t *testing.T) {
	workspace := filepath.Join(canonicalCompatibilityTestDir(t), "openclaw", "user-2", "instance-7")
	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 200007, 200007); err != nil {
		t.Fatalf("fresh first start must delegate workspace creation to runtime: %v", err)
	}
	message := "database disk image is malformed"
	if moved, err := quarantineCorruptLegacyOpenClawTaskState(workspace, 1, &message); err != nil || moved {
		t.Fatalf("missing legacy state must be a no-op: %v %v", moved, err)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("backend compatibility check created an instance workspace")
	}
}

func TestEnsureOpenClawPluginLayoutCompatibilityLeavesLegacyLayoutUntouched(t *testing.T) {
	workspace := canonicalCompatibilityTestDir(t)
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
		skipIfTestSymlinkUnavailable(t, err)
		t.Fatal(err)
	}

	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err != nil {
		skipIfTestSymlinkUnavailable(t, err)
		t.Fatalf("ensureOpenClawPluginLayoutCompatibility returned error: %v", err)
	}

	info, err := os.Lstat(legacyLink)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy plugin link changed: info=%v err=%v", info, err)
	}
}

func TestEnsureOpenClawPluginLayoutCompatibilityLinksProjectPackages(t *testing.T) {
	workspace := canonicalCompatibilityTestDir(t)
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
		skipIfTestSymlinkUnavailable(t, err)
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
	workspace := canonicalCompatibilityTestDir(t)
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
	workspace := canonicalCompatibilityTestDir(t)
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
	workspace := canonicalCompatibilityTestDir(t)
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
	workspace := canonicalCompatibilityTestDir(t)
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

func canonicalCompatibilityTestDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenClawCompatibilityRejectsPluginParentEscape(t *testing.T) {
	for _, parent := range []string{"home", "npm"} {
		t.Run(parent, func(t *testing.T) {
			workspace := canonicalCompatibilityTestDir(t)
			victim := canonicalCompatibilityTestDir(t)
			var linked, plugin string
			if parent == "home" {
				linked = filepath.Join(workspace, "home")
				plugin = filepath.Join(victim, ".openclaw", "npm", "projects", "plugin", "node_modules", "plugin")
			} else {
				linked = filepath.Join(workspace, "home", ".openclaw", "npm")
				plugin = filepath.Join(victim, "projects", "plugin", "node_modules", "plugin")
			}
			if err := os.MkdirAll(filepath.Dir(linked), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(plugin, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(plugin, "openclaw.plugin.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, linked); err != nil {
				t.Fatal(err)
			}
			err := ensureOpenClawPluginLayoutCompatibility(workspace, os.Getuid(), os.Getgid())
			if err == nil {
				t.Error("accepted tenant link outside workspace")
			}
			npm := victim
			if parent == "home" {
				npm = filepath.Join(victim, ".openclaw", "npm")
			}
			if _, err := os.Lstat(filepath.Join(npm, "node_modules")); !os.IsNotExist(err) {
				t.Fatalf("external victim was modified: %v", err)
			}
		})
	}
}

func TestOpenClawCompatibilityRejectsTaskAndQuarantineEscape(t *testing.T) {
	for _, parent := range []string{"tasks", "quarantine"} {
		t.Run(parent, func(t *testing.T) {
			workspace := canonicalCompatibilityTestDir(t)
			victim := canonicalCompatibilityTestDir(t)
			home := filepath.Join(workspace, "home", ".openclaw")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, filepath.Join(home, parent)); err != nil {
				t.Fatal(err)
			}
			tasks := filepath.Join(home, "tasks")
			if parent == "tasks" {
				tasks = victim
			} else if err := os.MkdirAll(tasks, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tasks, "runs.sqlite"), []byte("recoverable"), 0o600); err != nil {
				t.Fatal(err)
			}
			message := "SQLITE_CORRUPT"
			moved, err := quarantineCorruptLegacyOpenClawTaskState(workspace, 12, &message)
			if err == nil || moved {
				t.Errorf("escape accepted: moved=%v err=%v", moved, err)
			}
			if data, err := os.ReadFile(filepath.Join(tasks, "runs.sqlite")); err != nil || string(data) != "recoverable" {
				t.Fatalf("source victim changed: %q %v", data, err)
			}
			if parent == "quarantine" {
				entries, err := os.ReadDir(victim)
				if err != nil || len(entries) != 0 {
					t.Fatalf("external quarantine modified: %v %v", entries, err)
				}
			}
		})
	}
}

func TestOpenClawCompatibilityRejectsInitialWorkspaceAncestorLink(t *testing.T) {
	pool := filepath.Join(canonicalCompatibilityTestDir(t), "openclaw")
	victim := canonicalCompatibilityTestDir(t)
	if err := os.MkdirAll(pool, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(pool, "user-42")); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(pool, "user-42", "instance-7")
	tasks := filepath.Join(victim, "instance-7", "home", ".openclaw", "tasks")
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasks, "runs.sqlite"), []byte("victim"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureOpenClawPluginLayoutCompatibility(workspace, os.Getuid(), os.Getgid()); err == nil {
		t.Error("plugin preparation accepted redirected workspace ancestor")
	}
	message := "database is corrupt"
	if moved, err := quarantineCorruptLegacyOpenClawTaskState(workspace, 7, &message); err == nil || moved {
		t.Errorf("quarantine accepted redirected workspace ancestor: %v %v", moved, err)
	}
	if data, err := os.ReadFile(filepath.Join(tasks, "runs.sqlite")); err != nil || string(data) != "victim" {
		t.Fatalf("sibling instance victim changed: %q %v", data, err)
	}
}

func TestOpenClawCompatibilityPreservesImageLinkAndBoundsManifestLookup(t *testing.T) {
	workspace := canonicalCompatibilityTestDir(t)
	image := canonicalCompatibilityTestDir(t)
	npm := filepath.Join(workspace, "home", ".openclaw", "npm")
	modules := filepath.Join(npm, "projects", "project", "node_modules")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(image, "openclaw.plugin.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(modules, "image-plugin")
	if err := os.Symlink(image, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureOpenClawPluginLayoutCompatibility(workspace, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("linked image dependency must be skipped without following it: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(npm, "node_modules", "image-plugin")); !os.IsNotExist(err) {
		t.Fatalf("external image package was published as a workspace plugin: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(image, "openclaw.plugin.json")); err != nil || string(data) != "{}" {
		t.Fatalf("external image manifest changed: %q %v", data, err)
	}
	if got, err := os.Readlink(link); err != nil || got != image {
		t.Fatalf("image link changed: %q %v", got, err)
	}
	// An existing global image layout is authoritative; never traverse or chown it.
	global := filepath.Join(npm, "node_modules")
	if err := os.Symlink(image, global); err != nil {
		t.Fatal(err)
	}
	if err := ensureOpenClawPluginLayoutCompatibility(workspace, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(global); err != nil || got != image {
		t.Fatalf("global image link changed: %q %v", got, err)
	}
}

func TestOpenClawCompatibilityConcurrentParentSwapStaysBounded(t *testing.T) {
	workspace := canonicalCompatibilityTestDir(t)
	victim := canonicalCompatibilityTestDir(t)
	for _, home := range []string{filepath.Join(workspace, "home"), victim} {
		plugin := filepath.Join(home, ".openclaw", "npm", "projects", "project", "node_modules", "plugin")
		tasks := filepath.Join(home, ".openclaw", "tasks")
		if err := os.MkdirAll(plugin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(tasks, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(plugin, "openclaw.plugin.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tasks, "runs.sqlite"), []byte("recoverable"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home, parked := filepath.Join(workspace, "home"), filepath.Join(workspace, "parked")
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if os.Rename(home, parked) != nil {
				continue
			}
			if os.Symlink(victim, home) == nil {
				runtime.Gosched()
				_ = os.Remove(home)
			}
			_ = os.Rename(parked, home)
		}
	}()
	message := "SQLITE_CORRUPT"
	for range 100 {
		_ = ensureOpenClawPluginLayoutCompatibility(workspace, os.Getuid(), os.Getgid())
		_, _ = quarantineCorruptLegacyOpenClawTaskState(workspace, 9, &message)
	}
	close(stop)
	<-done
	if _, err := os.Lstat(filepath.Join(victim, ".openclaw", "npm", "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("external plugin tree changed during swap: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(victim, ".openclaw", "quarantine")); !os.IsNotExist(err) {
		t.Fatalf("external quarantine tree changed during swap: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(victim, ".openclaw", "tasks", "runs.sqlite")); err != nil || string(data) != "recoverable" {
		t.Fatalf("external database changed during swap: %q %v", data, err)
	}
}
