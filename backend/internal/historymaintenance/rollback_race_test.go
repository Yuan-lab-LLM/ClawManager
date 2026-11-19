package historymaintenance

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

type pausedArchiveRead struct {
	Store
	entered chan struct{}
	resume  chan struct{}
}

func (s pausedArchiveRead) Read(ctx context.Context, owner string, ref []byte) ([]byte, error) {
	close(s.entered)
	select {
	case <-s.resume:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.Store.Read(ctx, owner, ref)
}

func rollbackFixture(t *testing.T) (*Service, Record) {
	t.Helper()
	db, restored := fixture(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -100)
	exec(t, db, `INSERT INTO model_invocations VALUES(1,12,3,'trace','session','completed',?,?,?)`, old, old, "body")
	s := New(db, &memoryArchive{objects: map[string][]byte{}}, nil)
	if _, err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyRestore(ctx, "model_invocations", 1, restored, "race-isolated-restore"); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `UPDATE history_archive_records SET summary_verified_at=UTC_TIMESTAMP(6),summary_sha256=payload_sha256`)
	month, _ := MonthlyTable("model_invocations", old)
	if _, complete, err := s.VerifyMonth(ctx, month, 0); err != nil || !complete {
		t.Fatalf("verify: %v %v", complete, err)
	}
	exec(t, db, `UPDATE history_maintenance_cursors SET after_id=0`)
	records, err := s.candidates(ctx, "model_invocations", time.Now().UTC().AddDate(0, 0, -90), 1)
	if err != nil || len(records) != 1 {
		t.Fatalf("candidate: %d %v", len(records), err)
	}
	r := records[0]
	r, err = s.verifyArchive(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}

func TestMySQLRollbackSerializesWithSourceDeletion(t *testing.T) {
	s, record := rollbackFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paused := pausedArchiveRead{Store: s.Store, entered: make(chan struct{}), resume: make(chan struct{})}
	worker := New(s.DB, paused, nil)
	type result struct {
		deleted bool
		err     error
	}
	done := make(chan result, 1)
	go func() { deleted, err := worker.deleteVerified(ctx, record); done <- result{deleted, err} }()
	select {
	case <-paused.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Another ordinary writer rolls back after the deletion worker inspected the
	// catalog, but before that worker has authenticated its object and begun SQL.
	err := s.RollbackMonth(ctx, record.Month)
	close(paused.resume)
	if err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.deleted {
		t.Fatalf("source deleted after successful rollback: deleted=%v err=%v", got.deleted, got.err)
	}
	if count(t, s.DB, "model_invocations") != 1 {
		t.Fatal("rollback lost source")
	}
}

func TestMySQLRollbackRechecksDeletionAfterCatalogLock(t *testing.T) {
	s, record := rollbackFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM history_month_catalog WHERE table_name=? FOR UPDATE`, record.Month).Scan(&state); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.RollbackMonth(ctx, record.Month) }()
	// Observe the actual blocked UPDATE/SELECT rather than assuming a sleep has
	// started the second transaction. Both versions must wait on this row.
	for {
		var waiting int
		err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.processlist WHERE DB=DATABASE() AND ID<>CONNECTION_ID() AND INFO LIKE '%history_month_catalog%'`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case e := <-done:
			t.Fatalf("rollback skipped catalog ownership: %v", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM model_invocations WHERE id=?`, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE history_archive_records SET source_deleted_at=UTC_TIMESTAMP(6) WHERE kind=? AND original_id=?`, record.Kind, record.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err == nil || !strings.Contains(err.Error(), "restoring deleted sources") {
		t.Fatalf("rollback accepted committed deletion: %v", err)
	}
}

func TestMySQLActiveMonthlyRetirementReleasesLocatorAndKeepsOldID(t *testing.T) {
	s, r := rollbackFixture(t)
	ctx := context.Background()
	if _, complete, err := s.VerifyLogMonth(ctx, r.Month, 0); err != nil || !complete {
		t.Fatalf("verify %v %v", complete, err)
	}
	if err := s.CutoverLogMonth(ctx, r.Month); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.deleteVerified(ctx, r); err != nil || !deleted {
		t.Fatalf("retire %v %v", deleted, err)
	}
	if count(t, s.DB, "history_log_locations") != 0 {
		t.Fatal("retired identity retained an online locator")
	}
	got, err := s.Get(ctx, r.Kind, r.ID, r.OwnerID, false)
	if err != nil || got.Status != "monthly" || string(got.Payload) != string(r.Payload) {
		t.Fatalf("retired old ID monthly route %+v %v", got, err)
	}
	if _, err = s.Get(ctx, r.Kind, r.ID, r.OwnerID+1, false); err == nil {
		t.Fatal("retired old ID crossed owner boundary")
	}
	exec(t, s.DB, "UPDATE `"+r.Month+"` SET payload_json='{}' WHERE original_id=?", r.ID)
	if _, err = s.Get(ctx, r.Kind, r.ID, r.OwnerID, false); err == nil {
		t.Fatal("corrupt retired active month silently fell back to archive")
	}
}

func TestMySQLStaleArchiveCannotReplaceCurrentMonthlyBody(t *testing.T) {
	s, r := rollbackFixture(t)
	ctx := context.Background()
	if _, complete, err := s.VerifyLogMonth(ctx, r.Month, 0); err != nil || !complete {
		t.Fatalf("verify %v %v", complete, err)
	}
	if err := s.CutoverLogMonth(ctx, r.Month); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = LockLogMonth(ctx, tx, r.Kind, r.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE model_invocations SET request_payload='current migrated body' WHERE id=?`, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = WriteLogBatch(ctx, tx, r.Kind, []any{r.ID}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.save(ctx, r); err == nil || !strings.Contains(err.Error(), "source changed") {
		t.Fatalf("stale source snapshot replaced current serving payload: %v", err)
	}
	got, err := s.Get(ctx, r.Kind, r.ID, r.OwnerID, false)
	if err != nil || !strings.Contains(string(got.Payload), "current migrated body") {
		t.Fatalf("current monthly body lost: %+v %v", got, err)
	}
}
