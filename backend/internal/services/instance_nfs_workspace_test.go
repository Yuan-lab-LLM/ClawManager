package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNFSWorkspaceAllocationDoesNotMutateSquashedBackendStorage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "unmounted")
	s := &instanceService{runtimeSandboxProfile: "nfs-rootless-v1"}
	got, err := s.allocateRuntimeWorkspace(root, "openclaw", 2, 7)
	if err != nil || got != filepath.Join(root, "openclaw/user-2/instance-7") {
		t.Fatalf("path=%s err=%v", got, err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("backend tried to provision NFS data using squashed root")
	}
	if _, err = s.allocateRuntimeWorkspace(root, "../other", 2, 7); err == nil {
		t.Fatal("accepted escaping runtime identity")
	}
	if _, err = s.allocateRuntimeWorkspace(root, "openclaw", 0, 7); err == nil {
		t.Fatal("accepted missing owner identity")
	}
}
