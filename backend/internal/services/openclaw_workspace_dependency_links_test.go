package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenClawCompatibilitySkipsLinkedDependenciesAndPublishesRealPlugins(t *testing.T) {
	for _, location := range []string{"image", "workspace"} {
		t.Run(location, func(t *testing.T) {
			workspace := canonicalCompatibilityTestDir(t)
			target := canonicalCompatibilityTestDir(t)
			if location == "workspace" {
				target = filepath.Join(workspace, "sdk")
			}
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "openclaw.plugin.json"), []byte("{}"), 0644); err != nil {
				t.Fatal(err)
			}
			npm := filepath.Join(workspace, "home", ".openclaw", "npm")
			modules := filepath.Join(npm, "projects", "project", "node_modules")
			for _, name := range []string{"real-plugin", "@scope/real-plugin"} {
				dir := filepath.Join(modules, filepath.FromSlash(name))
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "openclaw.plugin.json"), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			linked := []string{"es-object-atoms", "@scope/linked-sdk"}
			for _, name := range linked {
				if err := os.Symlink(target, filepath.Join(modules, filepath.FromSlash(name))); err != nil {
					skipIfTestSymlinkUnavailable(t, err)
					t.Fatal(err)
				}
			}
			global := filepath.Join(npm, "node_modules")
			if err := os.MkdirAll(global, 0755); err != nil {
				t.Fatal(err)
			}
			legacy := filepath.Join(global, "legacy-image-plugin")
			if err := os.Symlink(target, legacy); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"real-plugin", "@scope/real-plugin"} {
				resolved, err := filepath.EvalSymlinks(filepath.Join(global, filepath.FromSlash(name)))
				if err != nil || resolved != filepath.Join(modules, filepath.FromSlash(name)) {
					t.Fatalf("real plugin not linked: %s %s %v", name, resolved, err)
				}
			}
			for _, name := range linked {
				if _, err := os.Lstat(filepath.Join(global, filepath.FromSlash(name))); !os.IsNotExist(err) {
					t.Fatalf("linked dependency published: %s %v", name, err)
				}
				if got, err := os.Readlink(filepath.Join(modules, filepath.FromSlash(name))); err != nil || got != target {
					t.Fatalf("dependency changed: %s %v", got, err)
				}
			}
			if got, err := os.Readlink(legacy); err != nil || got != target {
				t.Fatalf("legacy image link changed: %s %v", got, err)
			}
		})
	}
}

func TestOpenClawCompatibilityStillRejectsEscapingManifestInRealPackage(t *testing.T) {
	workspace := canonicalCompatibilityTestDir(t)
	external := filepath.Join(canonicalCompatibilityTestDir(t), "openclaw.plugin.json")
	if err := os.WriteFile(external, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(workspace, "home", ".openclaw", "npm", "projects", "project", "node_modules", "real-plugin")
	if err := os.MkdirAll(pkg, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(pkg, "openclaw.plugin.json")); err != nil {
		skipIfTestSymlinkUnavailable(t, err)
		t.Fatal(err)
	}
	if err := ensureOpenClawPluginLayoutCompatibility(workspace, 0, 0); err == nil {
		t.Fatal("real package manifest escaped workspace")
	}
}
