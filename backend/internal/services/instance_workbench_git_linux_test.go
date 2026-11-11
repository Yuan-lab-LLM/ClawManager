//go:build linux

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNFSWorkspaceGitReadBoundsAndPrivateInodes(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential helper requires root")
	}
	base, e := os.MkdirTemp("/tmp", "cm-git-read-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(base)
	_ = os.Chmod(base, 0755)
	root := filepath.Join(base, "opencode/user-3/instance-12")
	_ = os.MkdirAll(root, 0755)
	_ = os.Chown(root, 200012, 200012)
	for name, data := range map[string]string{"extensionless": "text\nlast", "binary": "\xff\x00raw", "large": strings.Repeat("x", (1<<20)+1), "wrong-owner": "secret"} {
		p := filepath.Join(root, name)
		if e = os.WriteFile(p, []byte(data), 0644); e != nil {
			t.Fatal(e)
		}
		if name != "wrong-owner" {
			_ = os.Chown(p, 200012, 200012)
		}
	}
	_ = os.Symlink("extensionless", filepath.Join(root, "link"))
	_ = os.Link(filepath.Join(root, "extensionless"), filepath.Join(root, "hardlink"))
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: root}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, &workspaceWriteFake{})
	data, e := s.ReadGitFile(context.Background(), scope, "binary")
	if e != nil || string(data) != "\xff\x00raw" {
		t.Fatalf("binary roundtrip %q %v", data, e)
	}
	for _, name := range []string{"large", "wrong-owner", "link", "hardlink", "../binary"} {
		if _, e = s.ReadGitFile(context.Background(), scope, name); e == nil {
			t.Fatalf("accepted unsafe file %s", name)
		}
	}
	if _, e = s.ReadGitFile(context.Background(), scope, "large"); !errors.Is(e, ErrWorkspacePreviewTooLarge) {
		t.Fatalf("lost size error %v", e)
	}
	_ = os.Remove(filepath.Join(root, "hardlink"))
	data, e = s.ReadGitFile(context.Background(), scope, "extensionless")
	if e != nil || string(data) != "text\nlast" {
		t.Fatalf("text read %q %v", data, e)
	}
}

func TestNFSWorkspaceGitConfigCooperativeLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential helper requires root")
	}
	base, e := os.MkdirTemp("/tmp", "cm-git-lock-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(base)
	_ = os.Chmod(base, 0755)
	root := filepath.Join(base, "opencode/user-3/instance-12")
	metadata := filepath.Join(root, ".git")
	_ = os.MkdirAll(metadata, 0755)
	for _, p := range []string{root, metadata} {
		_ = os.Chown(p, 200012, 200012)
	}
	config := filepath.Join(metadata, "config")
	_ = os.WriteFile(config, []byte("[core]\n bare=false\n# unrelated bytes\n"), 0600)
	_ = os.Chown(config, 200012, 200012)
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: root}
	writes := &workspaceWriteFake{}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes)
	doc, e := s.ReadText(context.Background(), scope, ".git/config")
	if e != nil {
		t.Fatal(e)
	}
	audited := false
	s.auditHook = func(_, _ string, _ int64) error {
		audited = true
		if !writes.active {
			return fmt.Errorf("Git save escaped NFS write fence")
		}
		info, e := os.Stat(config + ".lock")
		if e != nil {
			return fmt.Errorf("missing Git lock during NFS audit: %v", e)
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != 200012 {
			return fmt.Errorf("Git lock created outside business UID")
		}
		out, e := exec.Command("git", "config", "--file", config, "review.concurrent", "external").CombinedOutput()
		if e == nil || !strings.Contains(string(out), "lock") {
			return fmt.Errorf("NFS Git save did not exclude Git writer: %q %v", out, e)
		}
		return nil
	}
	updated := doc.Text + "[user]\n name=Person\n email=person@example.test\n"
	if _, e = s.SaveText(context.Background(), scope, ".git/config", updated, doc.Version); e != nil {
		t.Fatal(e)
	}
	if !audited || writes.active || writes.begins != 1 || writes.completes != 1 {
		t.Fatalf("NFS write fence lifecycle mismatch %+v", writes)
	}
	if _, e = os.Lstat(config + ".lock"); !os.IsNotExist(e) {
		t.Fatal("NFS own lock leaked", e)
	}
	s.auditHook = nil
	doc, e = s.ReadText(context.Background(), scope, ".git/config")
	if e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(config+".lock", []byte("other lock"), 0600)
	if _, e = s.SaveText(context.Background(), scope, ".git/config", "changed", doc.Version); !errors.Is(e, ErrWorkspaceTextConflict) {
		t.Fatalf("NFS lock conflict lost: %v", e)
	}
	if data, _ := os.ReadFile(config + ".lock"); string(data) != "other lock" {
		t.Fatal("NFS removed existing lock")
	}
}
