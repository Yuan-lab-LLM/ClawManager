package systembackup

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	appdb "clawreef/internal/db"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"clawreef/internal/systembackup/team"
	uppermysql "github.com/upper/db/v4/adapter/mysql"
)

func TestWholeBranchNativeRestorePreservesRetiredPublicIDFloor(t *testing.T) {
	source := fullSchemaDB(t)
	sess := appdb.Session
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	store := testObjectStore(t)
	encrypted, e := observability.NewEncryptedObjects(historyTestObjects{store, "allocator/"}, bytes.Repeat([]byte{92}, 32))
	must(e)
	history := historymaintenance.New(source, wholeNativeArchive{encrypted, false}, nil)
	old := time.Now().UTC().AddDate(0, 0, -100).Truncate(time.Second)
	_, e = source.Exec(`INSERT INTO users(id,username,email,password_hash,role) VALUES(42,'allocator','allocator@test.invalid','fixture','user')`)
	must(e)
	owner := 42
	repo := repository.NewModelInvocationRepository(sess)
	live := &models.ModelInvocation{UserID: &owner, TraceID: "live", RequestID: "live", ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: time.Now().UTC()}
	must(repo.Create(live))
	retired := &models.ModelInvocation{UserID: &owner, TraceID: "retired", RequestID: "retired", ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: old, CompletedAt: &old}
	must(repo.Create(retired))
	_, n, e := history.Backfill(ctx, "model_invocations", old, old.Add(time.Hour), 0, 500)
	must(e)
	if n != 1 {
		t.Fatalf("archive %d", n)
	}
	_, e = repository.ReconcileObservationSummaries(ctx, sess, 500)
	must(e)
	proofName, e := RandomID("cm_history_restore_")
	must(e)
	_, e = source.Exec("CREATE DATABASE `" + proofName + "`")
	must(e)
	proof, e := sql.Open("mysql", "root@tcp(127.0.0.1:"+os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT")+")/"+proofName+"?parseTime=true")
	must(e)
	t.Cleanup(func() { proof.Close(); source.Exec("DROP DATABASE `" + proofName + "`") })
	must(history.VerifyRestore(ctx, "model_invocations", int64(retired.ID), proof, "actual-isolated-retirement-proof"))
	month, _ := historymaintenance.MonthlyTable("model_invocations", old)
	_, complete, e := history.VerifyMonth(ctx, month, 0)
	must(e)
	if !complete {
		t.Fatal("archive verify incomplete")
	}
	_, complete, e = history.VerifyLogMonth(ctx, month, 0)
	must(e)
	if !complete {
		t.Fatal("route verify incomplete")
	}
	must(history.CutoverLogMonth(ctx, month))
	result, e := history.Tick(ctx)
	must(e)
	if result.Deleted != 1 {
		t.Fatalf("verified source retirement %+v", result)
	}
	var count int
	must(source.QueryRow(`SELECT COUNT(*) FROM model_invocations WHERE id=?`, retired.ID).Scan(&count))
	if count != 0 {
		t.Fatal("retired source retained")
	}
	tables, e := SnapshotBusinessTables(ctx, source)
	must(e)
	var raw, normalized bytes.Buffer
	_, e = ExportMySQL(ctx, source, &raw, tables)
	must(e)
	_, e = NormalizeSQLSnapshot(ctx, bytes.NewReader(raw.Bytes()), &normalized, time.Now().UTC(), func(SQLTable, team.Change) error { return nil })
	must(e)
	target, name := isolatedSchemaDB(t, source)
	// Prime MySQL's stale information_schema statistics before trusted ALTER;
	// the allocator verifier must read current engine metadata instead.
	var cached uint64
	must(target.QueryRow(`SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='model_invocations'`).Scan(&cached))
	must(prepareMonthlyRestoreSchema(ctx, target, bytes.NewReader(normalized.Bytes())))
	_, e = RestoreMySQL(ctx, target, "source", name, bytes.NewReader(normalized.Bytes()), nil)
	must(e)
	_, e = VerifyMySQL(ctx, target, bytes.NewReader(normalized.Bytes()), 10000)
	must(e)
	nextID, e := readHistoryAllocator(ctx, target, "model_invocations")
	must(e)
	if !nextID.Valid || nextID.Int64 <= int64(retired.ID) {
		t.Fatalf("native restored allocator would reuse retired public ID: next=%+v archived=%d live=%d", nextID, retired.ID, live.ID)
	}
	// The fixture supplies only fresh immutable lock slots for one repository
	// write probe. It never starts business services or installs executable hooks
	// in the isolated restore database, and no captured authority is replayed.
	_, e = target.Exec(`INSERT INTO observation_summary_day_locks(day_slot) WITH RECURSIVE slots AS(SELECT 0 n UNION ALL SELECT n+1 FROM slots WHERE n<255) SELECT n FROM slots`)
	must(e)
	restored, e := uppermysql.Open(uppermysql.ConnectionURL{Host: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), Database: name, User: "root"})
	must(e)
	defer restored.Close()
	reader := historymaintenance.New(restored.Driver().(*sql.DB), wholeNativeArchive{encrypted, false}, nil)
	repository.ConfigureHistoryReader(restored, reader)
	defer repository.ConfigureHistoryReader(restored, nil)
	next := &models.ModelInvocation{UserID: &owner, TraceID: "after-restore", RequestID: "after-restore", ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: time.Now().UTC()}
	must(repository.NewModelInvocationRepository(restored).Create(next))
	if next.ID <= retired.ID {
		t.Fatalf("new Create reused retired public ID: new=%d archived=%d live=%d", next.ID, retired.ID, live.ID)
	}
	got, e := repository.NewModelInvocationRepository(restored).GetByID(retired.ID)
	must(e)
	if got == nil || got.TraceID != "retired" {
		t.Fatalf("old ID now returns %+v", got)
	}
	t.Logf("actual highest-ID retirement, native restore and repository Create: live=%d archived=%d new=%d", live.ID, retired.ID, next.ID)
}
