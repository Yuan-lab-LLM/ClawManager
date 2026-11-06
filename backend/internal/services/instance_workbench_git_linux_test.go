//go:build linux

package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
