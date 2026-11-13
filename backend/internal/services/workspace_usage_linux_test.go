//go:build linux

package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceUsageTreeCompleteOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "data"), []byte("1234567"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private"), []byte("must not count"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	n, err := measureWorkspaceUsageTree(context.Background(), root, 100)
	if err != nil || n != 7 {
		t.Fatalf("nested logical bytes=%d err=%v", n, err)
	}
	n, err = measureWorkspaceUsageTree(context.Background(), t.TempDir(), 100)
	if err != nil || n != 0 {
		t.Fatalf("empty bytes=%d err=%v", n, err)
	}
	for _, tc := range []struct {
		name, path string
		limit      int
		ctx        context.Context
	}{
		{"missing", filepath.Join(root, "missing"), 100, context.Background()},
		{"truncated", root, 1, context.Background()},
		{"cancelled", root, 100, func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := measureWorkspaceUsageTree(tc.ctx, tc.path, tc.limit)
			if err == nil || n != 0 {
				t.Fatalf("incomplete scan published subtotal %d err=%v", n, err)
			}
		})
	}
}

func TestWorkspaceUsageCredentialHelper(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential switching requires trusted root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-usage-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err = os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "openclaw", "user-3", "instance-12")
	if err = os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(path, RuntimeLinuxID(12), RuntimeLinuxID(12)); err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, nil)
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 2, WorkspacePath: path}
	n, err := s.MeasureWorkspaceUsage(t.Context(), scope)
	if err != nil || n != 0 {
		t.Fatalf("empty credential scan %d %v", n, err)
	}
	file := filepath.Join(path, "private")
	if err = os.WriteFile(file, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(file, RuntimeLinuxID(12), RuntimeLinuxID(12)); err != nil {
		t.Fatal(err)
	}
	n, err = s.MeasureWorkspaceUsage(t.Context(), scope)
	if err != nil || n != 5 {
		t.Fatalf("private credential scan %d %v", n, err)
	}
	// Exercise the production constructor binding, with a stopped retained
	// workspace and the real credential helper, without any runtime service.
	store := &usageTestStore{items: []models.Instance{{ID: 12, UserID: 3, Status: "stopped", RuntimeGeneration: 2, WorkspacePath: &path}}}
	collector := NewWorkspaceUsageCollector(nil, s)
	collector.store = store
	collector.collect(t.Context())
	if len(store.sizes) != 1 || store.sizes[0] != 5 {
		t.Fatal("collector did not use real helper", store.sizes)
	}
	if err = os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	n, err = s.MeasureWorkspaceUsage(t.Context(), scope)
	if err == nil || n != 0 {
		t.Fatalf("permission failure %d %v", n, err)
	}
	if err = os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	n, err = s.MeasureWorkspaceUsage(ctx, scope)
	if !errors.Is(err, context.Canceled) || n != 0 || time.Since(start) > time.Second {
		t.Fatalf("cancel not honored %d %v", n, err)
	}
}

func TestWorkspaceUsageHelperProcessDeadlineAndBudget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential switching requires trusted root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-usage-cancel-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err = os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "openclaw", "user-3", "instance-12")
	if err = os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(path, RuntimeLinuxID(12), RuntimeLinuxID(12)); err != nil {
		t.Fatal(err)
	}
	scope := WorkspaceFileScope{InstanceID: 12, UserID: 3, RuntimeGeneration: 2, WorkspacePath: path}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	start := time.Now()
	_, file, ended, err := runWorkspaceNFSHelper(ctx, workspaceNFSRequest{Action: "measure_usage", Root: base, Scope: scope, UID: RuntimeLinuxID(12), GID: RuntimeLinuxID(12), Limit: 100000, UploadMax: 1}, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !ended || file != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("helper deadline did not reap process: ended=%v err=%v", ended, err)
	}
	for i := 0; i < cap(workspaceUsageHelpers); i++ {
		workspaceUsageHelpers <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(workspaceUsageHelpers); i++ {
			<-workspaceUsageHelpers
		}
	}()
	s := NewWorkspaceFileService(nil).(*workspaceFileService)
	ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", base, nil)
	if n, err := s.MeasureWorkspaceUsage(t.Context(), scope); err == nil || n != 0 {
		t.Fatal("exhausted helper budget spawned work", n, err)
	}
}
