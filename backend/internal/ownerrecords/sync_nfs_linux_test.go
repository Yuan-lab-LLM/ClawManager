//go:build linux

package ownerrecords

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryReaderGroupWaitsForLiveDescendant(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 1 &")
	configureHistoryReader(cmd)
	if e := cmd.Run(); e != nil {
		t.Fatal(e)
	}
	if historyReaderGroupDrained(cmd) {
		t.Fatal("a live descendant was treated as reaped")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !historyReaderGroupDrained(cmd) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !historyReaderGroupDrained(cmd) {
		t.Fatal("fully exited process group never released")
	}
}

// Explicit opt-in, task-owned local NFS export only. The orchestrator pauses
// that export between preparation and this outage test. No user mounts used.
func TestSyncNFSPrepare(t *testing.T) {
	root := os.Getenv("PORTAL_SYNC_TEST_NFS_ROOT")
	if root == "" {
		t.Skip("isolated NFS not configured")
	}
	ws := filepath.Join(root, "openclaw/user-99/instance-1")
	if e := os.MkdirAll(filepath.Join(ws, "home"), 0700); e != nil {
		t.Fatal(e)
	}
	s := &Store{HistoryRoot: root}
	r, e := s.isolatedHistory(context.Background(), historyReadRequest{Operation: "empty", Root: root, Workspace: ws, Identity: Identity{Scope: 99, Instance: 1, Runtime: "openclaw"}})
	if e != nil || !r.Empty {
		t.Fatal("healthy NFS fixture unavailable", r, e)
	}
}

func TestSyncNFSOutageDoesNotBlockSiblingStages(t *testing.T) {
	root := os.Getenv("PORTAL_SYNC_TEST_NFS_ROOT")
	if root == "" || os.Getenv("PORTAL_SYNC_TEST_NFS_PAUSED") != "1" {
		t.Skip("isolated NFS outage not configured")
	}
	s := &Store{HistoryRoot: root}
	ctx, run := newSyncRun(context.Background(), time.Now())
	continued := false
	started := time.Now()
	runSyncPhases(ctx, run, []syncPhase{
		{name: "history", budget: 300 * time.Millisecond, run: func(c context.Context) error {
			_, e := s.isolatedHistory(c, historyReadRequest{Operation: "empty", Root: root, Workspace: filepath.Join(root, "openclaw/user-99/instance-1"), Identity: Identity{Scope: 99, Instance: 1, Runtime: "openclaw"}})
			if !errors.Is(e, context.DeadlineExceeded) {
				t.Errorf("expected actual blocked NFS read deadline, got %v", e)
			}
			return e
		}},
		{name: "projection", run: func(c context.Context) error { continued = c.Err() == nil; return nil }},
	}, nil)
	if !continued || time.Since(started) > 2*time.Second || len(run.Stages) != 2 {
		t.Fatal("NFS outage stopped siblings", time.Since(started), continued)
	}
	if run.Stages[0].Reason != "budget" || run.Stages[1].State != "success" {
		t.Fatal(run.Stages)
	}
	if len(nativeReaderSlots) > 1 {
		t.Fatal("unbounded reader admission")
	}
	t.Logf("real_NFS_outage elapsed=%s sibling=success inflight=%d", time.Since(started), len(nativeReaderSlots))
}
