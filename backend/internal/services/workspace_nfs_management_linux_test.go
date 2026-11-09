//go:build linux

package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNFSWorkspaceManagementProbeAndErase(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires credential switching")
	}
	base, err := os.MkdirTemp("/tmp", "cm-management-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	workspace := filepath.Join(base, "opencode", "user-3", "instance-12")
	if err = os.MkdirAll(filepath.Join(workspace, "home"), 0755); err != nil {
		t.Fatal(err)
	}
	os.Chown(filepath.Dir(workspace), 10001, 10001)
	os.Chown(workspace, 10001, 200012)
	os.Chmod(workspace, 0770)
	home := filepath.Join(workspace, "home")
	os.Chown(home, 200012, 200012)
	os.Chmod(home, 0700)
	os.WriteFile(filepath.Join(home, "private"), []byte("secret"), 0600)
	os.Chown(filepath.Join(home, "private"), 200012, 200012)
	outside := filepath.Join(base, "outside")
	os.WriteFile(outside, []byte("keep"), 0644)
	os.Symlink(outside, filepath.Join(home, "symlink"))
	instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: &workspace}
	if err = ProbeNFSInstanceWorkspace(t.Context(), base, "opencode", instance); err != nil {
		t.Fatal(err)
	}
	bad := *instance
	other := filepath.Join(base, "opencode", "user-3", "instance-13")
	bad.WorkspacePath = &other
	if err = ProbeNFSInstanceWorkspace(t.Context(), base, "opencode", &bad); err == nil {
		t.Fatal("mismatched scope accepted")
	}
	os.Remove(filepath.Join(home, "symlink"))
	os.Remove(filepath.Join(home, "private"))
	os.Remove(home)
	os.Symlink(base, home)
	if err = ProbeNFSInstanceWorkspace(t.Context(), base, "opencode", instance); err == nil {
		t.Fatal("symlink home accepted")
	}
	os.Remove(home)
	os.Mkdir(home, 0700)
	os.Chown(home, 200012, 200012)
	os.Symlink(outside, filepath.Join(home, "symlink"))
	writes := &workspaceWriteFake{}
	if err = EraseNFSInstanceWorkspace(t.Context(), base, "opencode", instance, writes); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("leaf remains: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatalf("escaped deletion: %q %v", data, err)
	}
	if writes.active || writes.begins != 1 || writes.completes != 1 {
		t.Fatalf("erase lifetime: %+v", writes)
	}
	if err = ProbeNFSInstanceWorkspace(t.Context(), base, "opencode", instance); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing probe: %v", err)
	}
	if _, err = os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("probe recreated leaf")
	}
	if err = EraseNFSInstanceWorkspace(t.Context(), base, "opencode", instance, writes); err != nil {
		t.Fatal("missing erase should be idempotent:", err)
	}
}

func TestNFSWorkspaceManagementBrowserConfig(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires credential switching")
	}
	base, err := os.MkdirTemp("/tmp", "cm-browser-management-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	workspace := filepath.Join(base, "openclaw", "user-3", "instance-12")
	configDir := filepath.Join(workspace, "home", ".openclaw")
	os.MkdirAll(configDir, 0755)
	for _, p := range []string{workspace, filepath.Join(workspace, "home"), configDir} {
		os.Chown(p, 200012, 200012)
		os.Chmod(p, 0700)
	}
	config := filepath.Join(configDir, "openclaw.json")
	os.WriteFile(config, []byte(`{"unrelated":"keep"}`), 0600)
	os.Chown(config, 200012, 200012)
	instance := &models.Instance{ID: 12, UserID: 3, Type: "openclaw", RuntimeGeneration: 4, WorkspacePath: &workspace}
	if _, err = PatchNFSBrowserConfig(context.Background(), base, instance, "clawbrowser-12.test.svc.cluster.local:9222", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(data), protectedBrowserURL) || !strings.Contains(string(data), "keep") {
		t.Fatalf("browser patch: %s %v", data, err)
	}
	if _, err = PatchNFSBrowserConfig(t.Context(), base, instance, "clawbrowser-13.test.svc.cluster.local:9222", true); err == nil {
		t.Fatal("wrong instance browser host accepted")
	}
}

func TestNFSWorkspaceManagementRealRootSquash(t *testing.T) {
	base := os.Getenv("CM_NFS_MANAGEMENT_ROOT")
	if base == "" {
		t.Skip("requires isolated preprovisioned root-squash NFS directory")
	}
	workspace := filepath.Join(base, "openclaw", "user-3", "instance-12")
	config := filepath.Join(workspace, "home", ".openclaw", "openclaw.json")
	if _, err := os.ReadFile(config); !os.IsPermission(err) {
		t.Fatalf("backend root negative control: %v", err)
	}
	instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: &workspace}
	if err := ProbeNFSInstanceWorkspace(t.Context(), base, "openclaw", instance); err != nil {
		t.Fatal(err)
	}
	if ended, err := PatchNFSBrowserConfig(t.Context(), base, instance, "clawbrowser-12.test.svc.cluster.local:9222", true); err != nil || !ended {
		t.Fatalf("private NFS browser config: ended=%v err=%v", ended, err)
	}
	writes := &workspaceWriteFake{}
	if err := EraseNFSInstanceWorkspace(t.Context(), base, "openclaw", instance, writes); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(workspace); !os.IsNotExist(err) {
		t.Fatalf("NFS leaf still exists: %v", err)
	}
	if writes.active || writes.begins != 1 || writes.completes != 1 {
		t.Fatalf("NFS erase intent: %+v", writes)
	}
	t.Log("root-squash denied backend root; private-home probe,0600 protected-browser config, business-UID clear and management-UID empty-leaf removal passed")
}

func TestNFSWorkspaceOperationLockTransfersPermanentInode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires credential switching")
	}
	base, err := os.MkdirTemp("/tmp", "cm-operation-lock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chown(base, 10001, 10001)
	os.Chmod(base, 0700)
	service := &RuntimeUpgradeService{workspaceRoot: base}
	ConfigureRuntimeUpgradeNFSIO(service, "nfs-rootless-v1")
	release, err := service.lockWorkspaceOperations([]int{12})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(base, ".clawmanager-upgrades", "operations", "instance-12.lock")
	before, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := service.lockWorkspaceOperations([]int{12}); err == nil {
		next()
		t.Fatal("SCM_RIGHTS did not preserve helper's lock")
	} else if !errors.Is(err, ErrWorkspaceUpgradeBusy) {
		t.Fatal(err)
	}
	release()
	next, err := service.lockWorkspaceOperations([]int{12})
	if err != nil {
		t.Fatal(err)
	}
	next()
	after, err := os.Stat(name)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("permanent operation inode changed")
	}
	os.Remove(name)
	os.Symlink(filepath.Join(base, "outside"), name)
	if next, err := service.lockWorkspaceOperations([]int{12}); err == nil {
		next()
		t.Fatal("symlink lock accepted")
	}
}

func TestNFSWorkspaceOperationLockRealRootSquashRuntime(t *testing.T) {
	root, binary := os.Getenv("CM_NFS_OPERATION_ROOT"), os.Getenv("CM_NFS_RUNTIME_LOCK_TEST_BINARY")
	if root == "" || binary == "" {
		t.Skip("requires isolated NFS and actual Runtime gateway test binary")
	}
	service := &RuntimeUpgradeService{workspaceRoot: root}
	ConfigureRuntimeUpgradeNFSIO(service, "nfs-rootless-v1")
	release, err := service.lockWorkspaceOperations([]int{12})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	run := func(busy string) {
		t.Helper()
		command := exec.Command(binary, "-test.run=^TestNFSBackendOperationLockProbe$", "-test.v")
		command.Env = []string{"PATH=/usr/bin:/bin", "CM_NFS_OPERATION_ROOT=" + root, "CM_NFS_OPERATION_EXPECT_BUSY=" + busy}
		command.Dir = "/"
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001, Groups: []uint32{}}}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Runtime lock probe: %s %v", output, err)
		}
	}
	run("1")
	release()
	release = func() {}
	run("0")
	t.Log("root-squash NFS: actual Runtime lock denied while CM held transferred FD, admitted after CM closed it")
}
