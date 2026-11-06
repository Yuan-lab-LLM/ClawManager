package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceTextGitConfigCooperativeLock(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "repo/.git"), 0700)
	config := filepath.Join(root, "repo/.git/config")
	_ = os.WriteFile(config, []byte("[core]\n bare=false\n# preserve\n"), 0600)
	scope := workspaceTestScope(root)
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	doc, e := s.ReadText(context.Background(), scope, "repo/.git/config")
	if e != nil {
		t.Fatal(e)
	}
	called := false
	s.auditHook = func(_, _ string, _ int64) error {
		called = true
		info, e := os.Stat(config + ".lock")
		if e != nil || info.Mode().Perm() != 0600 {
			return fmt.Errorf("Git config lock absent or wrong permissions: %v", e)
		}
		out, e := exec.Command("git", "config", "--file", config, "review.concurrent", "external").CombinedOutput()
		if e == nil || !strings.Contains(string(out), "lock") {
			return fmt.Errorf("concurrent git config was not fenced: %q %v", out, e)
		}
		return nil
	}
	updated := "[core]\n bare=false\n# preserve\n[user]\n name=Person\n"
	if _, e = s.SaveText(context.Background(), scope, "repo/.git/config", updated, doc.Version); e != nil {
		t.Fatal(e)
	}
	if !called {
		t.Fatal("audit fence was not exercised")
	}
	if data, _ := os.ReadFile(config); string(data) != updated {
		t.Fatal("CM save lost config bytes")
	}
	if _, e = os.Lstat(config + ".lock"); !os.IsNotExist(e) {
		t.Fatal("own config lock leaked", e)
	}
	s.auditHook = nil
	doc, e = s.ReadText(context.Background(), scope, "repo/.git/config")
	if e != nil {
		t.Fatal(e)
	}
	sentinel := "external Git lock"
	_ = os.WriteFile(config+".lock", []byte(sentinel), 0600)
	if _, e = s.SaveText(context.Background(), scope, "repo/.git/config", "changed", doc.Version); !errors.Is(e, ErrWorkspaceTextConflict) {
		t.Fatalf("preexisting Git lock did not conflict: %v", e)
	}
	if data, _ := os.ReadFile(config + ".lock"); string(data) != sentinel {
		t.Fatal("removed another writer's Git lock")
	}
	_ = os.Remove(config + ".lock")
	s.auditHook = func(_, _ string, _ int64) error { return errors.New("audit rejected") }
	if _, e = s.SaveText(context.Background(), scope, "repo/.git/config", "changed", doc.Version); e == nil {
		t.Fatal("ignored audit failure")
	}
	if _, e = os.Lstat(config + ".lock"); !os.IsNotExist(e) {
		t.Fatal("lock leaked after failed save", e)
	}
	s.auditHook = func(_, _ string, _ int64) error {
		if e := os.Remove(config + ".lock"); e != nil {
			return e
		}
		if e := os.WriteFile(config+".lock", []byte("replacement lock"), 0600); e != nil {
			return e
		}
		return errors.New("audit rejected after lock replacement")
	}
	if _, e = s.SaveText(context.Background(), scope, "repo/.git/config", "changed", doc.Version); e == nil {
		t.Fatal("ignored rejected audit")
	}
	if data, _ := os.ReadFile(config + ".lock"); string(data) != "replacement lock" {
		t.Fatal("cleanup removed another inode's lock")
	}
}
