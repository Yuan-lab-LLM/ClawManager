package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"clawreef/internal/systembackup"
)

func TestDeletedTeamClosureRequiresNativeCleanupAndCancelsWithoutDelivery(t *testing.T) {
	base := databaseReportingMySQL(t)
	row, err := base.SQL().QueryRow("SELECT DATABASE()")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = row.Scan(&name); err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(os.Getenv("TEST_DATABASE_MYSQL_PORT"))
	s, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repo := NewTeamRepository(s).(*teamRepository)
	ctx := context.Background()
	team := &models.Team{UserID: 1, Name: "deleted closure", Status: "deleted", CommunicationMode: "leader_mediated", RedisEventsLastID: "0-0"}
	if err = repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	if err = repo.PrepareTeamRedisCleanup(team, []int{}); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err == nil {
		t.Fatal("pending cleanup invented replay proof")
	}
	if _, err = s.SQL().Exec("UPDATE team_redis_cleanup_jobs SET state='archiving' WHERE team_id=?", team.ID); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err == nil {
		t.Fatal("default scan cursor was mistaken for completed discovery")
	}
	keyName := fmt.Sprintf("claw:team:%d:events", team.ID)
	if err = repo.DiscoverTeamRedisCleanupKeys(ctx, team.ID, "0", map[string]string{keyName: "stream"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err == nil {
		t.Fatal("unarchived key granted closure")
	}
	key := &TeamRedisCleanupKey{TeamID: team.ID, Key: keyName, Kind: "stream", Cursor: "0"}
	archiveHash := sha256.Sum256([]byte("verified archive fixture"))
	ref := []byte(fmt.Sprintf(`{"owner":"user:1","encryption_version":1,"sha256":"%x","object_key":"observability/v1/fixture/%x"}`, archiveHash, archiveHash))
	if err = repo.SaveTeamRedisArchive(ctx, key, "0", true, ref, []byte("verified archive fixture")); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err == nil {
		t.Fatal("archive without native deletion receipt granted closure")
	}
	if err = repo.MarkTeamRedisKeyDeleted(ctx, key); err != nil {
		t.Fatal(err)
	}
	member := &models.TeamMember{TeamID: team.ID, UserID: 1, MemberKey: "deleted", DisplayName: "Deleted", Role: "worker", Status: "deleted", Availability: "offline", InstanceMode: "lite", RuntimeType: "opencode"}
	if err = repo.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 501; i++ {
		if _, err = s.SQL().Exec(`INSERT INTO team_tasks(team_id,target_member_id,message_id,status,payload_json) VALUES(?,?,?,'pending','{"original":"retained"}')`, team.ID, member.ID, fmt.Sprintf("task-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err == nil {
		t.Fatal("unfinished tasks permitted replay closure")
	}
	if err = repo.CloseDeletedTeamWork(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	var settled int
	row, _ = s.SQL().QueryRow(`SELECT COUNT(*) FROM team_tasks WHERE team_id=? AND status='cancelled'`, team.ID)
	if err = row.Scan(&settled); err != nil || settled != 493 {
		t.Fatalf("work settlement batch exceeded/missed bound: %d %v", settled, err)
	}
	if err = repo.CloseDeletedTeamWork(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(`UPDATE team_tasks SET status='unknown_recovery',finished_at=NULL WHERE message_id='task-0' AND team_id=?`, team.ID); err != nil {
		t.Fatal(err)
	}
	if err = repo.CloseDeletedTeamWork(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err == nil {
		t.Fatal("unknown recovery state was silently terminated")
	}
	if _, err = s.SQL().Exec(`UPDATE team_tasks SET status='cancelled',finished_at=UTC_TIMESTAMP(6) WHERE message_id='task-0' AND team_id=?`, team.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 501; i++ {
		if _, err = s.SQL().Exec("INSERT INTO team_event_outbox(team_id,source_event_id,destination,message_id,payload_json,status) VALUES(?,?,'worker',?,'{\"original\":true}','pending')", team.ID, fmt.Sprint("source-", i), fmt.Sprint("message-", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	row, _ = s.SQL().QueryRow("SELECT COUNT(*) FROM history_replay_closures")
	if err = row.Scan(&n); err != nil || n != 0 {
		t.Fatal("partial cancellation granted closure", n, err)
	}
	for i := 0; i < 3; i++ {
		if err = repo.FinishTeamRedisCleanup(ctx, team.ID); err != nil {
			t.Fatal(err)
		}
	}
	for q, want := range map[string]int{"SELECT COUNT(*) FROM history_outbox_cancellations": 501, "SELECT COUNT(*) FROM history_outbox_receipts": 0, "SELECT COUNT(*) FROM history_replay_closures WHERE replay_closed AND recovery_closed AND idempotency_durable": 1, "SELECT COUNT(*) FROM team_event_outbox WHERE status='cancelled' AND delivered_at IS NULL": 501} {
		row, _ = s.SQL().QueryRow(q)
		if err = row.Scan(&n); err != nil || n != want {
			t.Fatalf("%s => %d want%d %v", q, n, want, err)
		}
	}
	replay := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: "source-0", Destination: "worker", MessageID: "message-0", PayloadJSON: `{"original":true}`}
	if err = repo.CreateEventOutbox(replay); err != nil || replay.Status != "cancelled" || replay.DeliveredAt != nil {
		t.Fatalf("cancelled replay changed business fact: %+v %v", replay, err)
	}
	if err = repo.MarkEventOutboxFailed(1, time.Now(), "stale worker"); err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkEventOutboxDelivered(1, time.Now()); err != nil {
		t.Fatal(err)
	}
	row, _ = s.SQL().QueryRow("SELECT COUNT(*) FROM team_event_outbox WHERE status='cancelled'")
	if err = row.Scan(&n); err != nil || n != 501 {
		t.Fatal("stale delivery reactivated cancelled work", n, err)
	}
	replay.PayloadJSON = `{"changed":true}`
	if err = repo.CreateEventOutbox(replay); err == nil {
		t.Fatal("cancellation receipt allowed identity reuse")
	}
	maintenance := historymaintenance.New(s.Driver().(*sql.DB), nil, nil)
	if result, err := maintenance.Tick(ctx); err != nil || result.Deleted != 0 {
		t.Fatalf("recent cancelled payload pruned: %+v %v", result, err)
	}
	for _, query := range []string{
		`UPDATE team_event_outbox SET created_at=UTC_TIMESTAMP(6)-INTERVAL 10 DAY`,
		`UPDATE history_outbox_cancellations SET cancelled_at=UTC_TIMESTAMP(6)-INTERVAL 8 DAY`,
	} {
		if _, err = s.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	gate := systembackup.Gate{DB: s.Driver().(*sql.DB)}
	if err = gate.EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	maintenance.Gate = func(ctx context.Context) (func(), error) {
		lease, err := gate.AcquireMutation(ctx, "installation", time.Minute)
		if err != nil {
			return nil, err
		}
		return func() {
			if err := gate.ReleaseMutation(context.Background(), lease); err != nil {
				t.Error(err)
			}
		}, nil
	}
	if _, err = s.SQL().Exec(`UPDATE system_backup_gate SET phase='held' WHERE scope='installation'`); err != nil {
		t.Fatal(err)
	}
	if _, err = maintenance.Tick(ctx); !errors.Is(err, systembackup.ErrMaintenance) {
		t.Fatalf("held backup gate ignored: %v", err)
	}
	row, _ = s.SQL().QueryRow(`SELECT COUNT(*) FROM team_event_outbox`)
	if err = row.Scan(&n); err != nil || n != 501 {
		t.Fatalf("closed native gate allowed deletion: %d %v", n, err)
	}
	if _, err = s.SQL().Exec(`UPDATE system_backup_gate SET phase='idle' WHERE scope='installation'`); err != nil {
		t.Fatal(err)
	}
	// A damaged receipt must keep the payload. The other 500 are independently
	// eligible and must fit exactly in a single native maintenance work unit.
	if _, err = s.SQL().Exec(`UPDATE history_outbox_cancellations SET source_event_id='SOURCE-500' WHERE message_id='message-500'`); err != nil {
		t.Fatal(err)
	}
	if result, err := maintenance.Tick(ctx); err != nil || result.Processed != 500 || result.Deleted != 500 {
		t.Fatalf("cancelled retention did not free bounded payloads: %+v %v", result, err)
	}
	if result, err := maintenance.Tick(ctx); err != nil || result.Deleted != 0 {
		t.Fatalf("mismatched source proof pruned: %+v %v", result, err)
	}
	if _, err = s.SQL().Exec(`UPDATE history_outbox_cancellations SET source_event_id='source-500' WHERE message_id='message-500'`); err != nil {
		t.Fatal(err)
	}
	if result, err := maintenance.Tick(ctx); err != nil || result.Deleted != 1 {
		t.Fatalf("verified remaining cancellation not reclaimed: %+v %v", result, err)
	}
	for q, want := range map[string]int{"SELECT COUNT(*) FROM team_event_outbox": 0, "SELECT COUNT(*) FROM history_outbox_cancellations": 501, "SELECT COUNT(*) FROM history_outbox_receipts": 0} {
		row, _ = s.SQL().QueryRow(q)
		if err = row.Scan(&n); err != nil || n != want {
			t.Fatalf("retention fact %s = %d want %d: %v", q, n, want, err)
		}
	}
	replay.PayloadJSON = `{"original":true}`
	if err = repo.CreateEventOutbox(replay); err != nil || replay.Status != "cancelled" || replay.DeliveredAt != nil {
		t.Fatalf("pruned cancellation resurrected: %+v %v", replay, err)
	}
	if err = repo.MarkEventOutboxFailed(1, time.Now(), "late"); err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkEventOutboxDelivered(1, time.Now()); err != nil {
		t.Fatal(err)
	}
	row, _ = s.SQL().QueryRow(`SELECT COUNT(*) FROM team_event_outbox`)
	if err = row.Scan(&n); err != nil || n != 0 {
		t.Fatalf("stale callbacks recreated pruned cancellation: %d %v", n, err)
	}
	replay.SourceEventID = "changed-source"
	if err = repo.CreateEventOutbox(replay); err == nil {
		t.Fatal("pruned cancellation source identity reused")
	}
}
