//go:build linux

package ownerrecords

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A real reader/SQLite-helper process chain, with synthetic helper latency.
// This does not emulate an actual NFS outage or touch any NFS configuration.
func TestIsolatedReaderSucceedsAfterOldTenSecondLimit(t *testing.T) {
	s, request := slowHelperFixture(t)
	started := time.Now()
	r, err := s.isolatedHistory(context.Background(), request)
	if err != nil || len(r.Questions) != 0 || time.Since(started) < 11*time.Second {
		t.Fatal("healthy twelve-second helper failed at an inner/outer old deadline", err, time.Since(started))
	}
}

func TestIsolatedReaderShutdownDoesNotWaitFullReadBudget(t *testing.T) {
	s, request := slowHelperFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := s.isolatedHistory(ctx, request)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatal("relaxed reader allowance ignored shorter parent cancellation", err, time.Since(started))
	}
	drain, release := context.WithTimeout(context.Background(), 3*time.Second)
	defer release()
	if !DrainHistoryReaders(drain) {
		t.Fatal("cancelled synthetic helper group did not drain")
	}
}

func slowHelperFixture(t *testing.T) (*Store, historyReadRequest) {
	t.Helper()
	root := t.TempDir()
	i := Identity{Scope: 1, Instance: 1, Runtime: "hermes", Mode: "lite"}
	ws := filepath.Join(root, "hermes/user-1/instance-1")
	dir := filepath.Join(ws, "home/.hermes")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.db"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sqlite3"), []byte("#!/bin/sh\nsleep 12\nprintf '[]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := &Store{HistoryRoot: root}
	return s, historyReadRequest{Operation: "read", Root: root, Workspace: ws, Identity: i}
}
