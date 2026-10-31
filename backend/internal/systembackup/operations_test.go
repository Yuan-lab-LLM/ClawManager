package systembackup

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestMutationOperationReplayIsAtomicAndRequestBound(t *testing.T) {
	ctx := context.Background()
	s := Store{DB: controlDB(t), InstallationID: "operation-test"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, _, e := s.SaveConfigIdempotent(ctx, 0, 1, "config-replay-0001", testConfig(), permitConfig)
			if e == nil && v.Version != 1 {
				e = errors.New("replay changed config version")
			}
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e := s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_config_versions").Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	if _, _, e := s.SaveConfigIdempotent(ctx, 1, 1, "config-replay-0001", testConfig(), permitConfig); PublicError(e) != "idempotency_conflict" {
		t.Fatal("different request replayed", e)
	}
	if _, _, e := s.SaveConfigIdempotent(ctx, 0, 1, "config-conflict-0002", testConfig(), permitConfig); PublicError(e) != "version_conflict" {
		t.Fatal(e)
	}
	if e := s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_operations").Scan(&count); e != nil || count != 1 {
		t.Fatal("failed mutation left committed operation", count, e)
	}
	// Fail the final ledger insert AFTER configuration and audit writes. All
	// effects must roll back together, so a response-loss retry is unambiguous.
	if _, e := s.DB.Exec("ALTER TABLE system_backup_operations ADD CONSTRAINT fixture_operation_failure CHECK(idempotency_key<>'blocked-key-0003')"); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.SaveConfigIdempotent(ctx, 1, 1, "blocked-key-0003", testConfig(), permitConfig); e == nil {
		t.Fatal("expected ledger failure")
	}
	current, e := s.Config(ctx)
	if e != nil || current.Version != 1 {
		t.Fatal("configuration survived failed operation transaction", current, e)
	}
	task, _, e := s.Create(ctx, 1, "operation-task-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if e != nil {
		t.Fatal(e)
	}
	first, replay, e := s.CancelIdempotent(ctx, task.ID, task.Version, 1, "cancel-replay-0001")
	if e != nil || replay || first.Status != "canceling" {
		t.Fatal(first, replay, e)
	}
	second, replay, e := s.CancelIdempotent(ctx, task.ID, task.Version, 1, "cancel-replay-0001")
	if e != nil || !replay || second.Version != first.Version || second.Status != first.Status {
		t.Fatal(second, replay, e)
	}
	if _, _, e = s.CancelIdempotent(ctx, task.ID, task.Version+1, 1, "cancel-replay-0001"); PublicError(e) != "idempotency_conflict" {
		t.Fatal(e)
	}
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_events WHERE task_id=? AND event_type='cancel_requested'", task.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate cancellation event", count, e)
	}
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_events WHERE event_type='config_saved'").Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate config audit", count, e)
	}
}
