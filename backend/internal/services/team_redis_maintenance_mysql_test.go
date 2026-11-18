package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
)

type maintenanceObjects struct {
	fail   bool
	values map[string][]byte
}

func (s *maintenanceObjects) PutObject(_ context.Context, key string, raw []byte, _ string) error {
	if s.fail {
		return errors.New("injected object outage")
	}
	s.values[key] = append([]byte(nil), raw...)
	return nil
}
func (s *maintenanceObjects) GetObject(_ context.Context, key string) ([]byte, error) {
	if s.fail {
		return nil, errors.New("injected object outage")
	}
	raw, ok := s.values[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), raw...), nil
}

func cleanupMaintenanceTeam(t *testing.T, b *redisBus, id int) {
	t.Helper()
	t.Cleanup(func() {
		cursor := "0"
		for {
			result, err := b.doRaw(context.Background(), "SCAN", cursor, "MATCH", fmt.Sprintf("claw:team:%d:*", id), "COUNT", "200")
			if err != nil {
				return
			}
			parts := result.([]interface{})
			cursor = parts[0].(string)
			for _, v := range parts[1].([]interface{}) {
				b.doRaw(context.Background(), "UNLINK", v.(string))
			}
			if cursor == "0" {
				return
			}
		}
	})
}

func TestDurableTeamDispatchRetryAfterRedisBeforeSQLDoesNotDuplicate(t *testing.T) {
	sess, repo, team, task, _ := projectionMySQL(t)
	b, _ := capacityRedisFixture(t)
	cleanupMaintenanceTeam(t, b, team.ID)
	task.Status = "pending"
	task.MessageID = "dispatch-fault"
	if err := repo.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"taskId":%d,"to":"worker","messageId":"dispatch-fault"}`, task.ID)
	out := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: "dispatch:dispatch-fault", Destination: teamInboxKey(team.ID, "worker"), MessageID: "dispatch-fault", PayloadJSON: payload}
	if err := repo.(repository.TeamDispatchOutboxRepository).EnqueueTeamDispatch(task, out); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec(`CREATE TRIGGER fail_dispatch_status BEFORE UPDATE ON team_tasks FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='dispatch SQL outage'`); err != nil {
		t.Fatal(err)
	}
	s := &teamService{repo: repo}
	if err := s.deliverTeamEventOutbox(team, b, out); err == nil {
		t.Fatal("injected status failure was not exercised")
	}
	if _, err := sess.SQL().Exec("DROP TRIGGER fail_dispatch_status"); err != nil {
		t.Fatal(err)
	}
	if err := s.deliverTeamEventOutbox(team, b, out); err != nil {
		t.Fatal(err)
	}
	if n, err := b.doRaw(context.Background(), "XLEN", out.Destination); err != nil || n != int64(1) {
		t.Fatal("durable retry duplicated accepted work", n, err)
	}
	entries, err := b.XRevRange(t.Context(), out.Destination, 1)
	if err != nil || len(entries) != 1 || entries[0].Fields["task_id"] != fmt.Sprint(task.ID) {
		t.Fatalf("legacy numeric dispatch wire identity changed: %v %v", entries, err)
	}
	got, err := repo.GetTaskByID(task.ID)
	if err != nil || got.Status != "dispatched" || got.RedisStreamID == nil {
		t.Fatal("task dispatch not recovered", got, err)
	}
}

func TestDurableTeamDispatchKeepsCanonicalWireIDAndFencesTaskScope(t *testing.T) {
	_, repo, team, task, _ := projectionMySQL(t)
	b, _ := capacityRedisFixture(t)
	cleanupMaintenanceTeam(t, b, team.ID)
	task.Status = models.TeamTaskStatusPending
	if err := repo.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	foreign := &models.Team{UserID: team.UserID, Name: "other-team", Status: "running", CommunicationMode: "leader_mediated", RedisEventsLastID: "0-0"}
	if err := repo.CreateTeam(foreign); err != nil {
		t.Fatal(err)
	}
	cleanupMaintenanceTeam(t, b, foreign.ID)
	member := &models.TeamMember{TeamID: foreign.ID, UserID: foreign.UserID, MemberKey: "leader", DisplayName: "Foreign", Role: "leader", RuntimeType: "openclaw", InstanceMode: "lite", Status: "idle", Availability: "idle"}
	if err := repo.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	foreignTask := &models.TeamTask{TeamID: foreign.ID, TargetMemberID: member.ID, MessageID: task.MessageID, Status: models.TeamTaskStatusPending, WorkflowState: "planning", PayloadJSON: "{}"}
	if err := repo.CreateTask(foreignTask); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	destination := teamInboxKey(team.ID, "leader")
	canonical := fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
	s := &teamService{repo: repo}
	outbox := func(taskRef, messageID, stream string) *models.TeamEventOutbox {
		return &models.TeamEventOutbox{
			TeamID: team.ID, SourceEventID: "dispatch:" + messageID, Destination: stream, MessageID: messageID,
			PayloadJSON: fmt.Sprintf(`{"taskId":%s,"to":"leader","messageId":%q}`, taskRef, messageID),
		}
	}
	for _, test := range []struct {
		name, taskRef, messageID, stream string
	}{
		{"foreign-reference", strconv.Quote(fmt.Sprintf("team-%d-task-%d", foreign.ID, task.ID)), task.MessageID, destination},
		{"foreign-task", strconv.Quote(fmt.Sprintf("team-%d-task-%d", team.ID, foreignTask.ID)), task.MessageID, destination},
		{"malformed", strconv.Quote(canonical + "-extra"), task.MessageID, destination},
		{"wrong-message", strconv.Quote(canonical), "other-message", destination},
		{"foreign-destination", strconv.Quote(canonical), task.MessageID, teamInboxKey(foreign.ID, "leader")},
		{"fractional-number", fmt.Sprintf("%d.75", task.ID), task.MessageID, destination},
		{"exponent-number", fmt.Sprintf("%de0", task.ID), task.MessageID, destination},
		{"signed-canonical", strconv.Quote(fmt.Sprintf("team-%d-task-+%d", team.ID, task.ID)), task.MessageID, destination},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := s.deliverTeamEventOutbox(team, b, outbox(test.taskRef, test.messageID, test.stream)); err == nil {
				t.Fatal("invalid dispatch scope accepted")
			}
			entries, err := b.XRevRange(ctx, test.stream, 2)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid dispatch wrote %d stream entries: %v", len(entries), err)
			}
		})
	}
	t.Run("missing-outbox-message", func(t *testing.T) {
		missing := outbox(strconv.Quote(canonical), task.MessageID, destination)
		missing.MessageID = ""
		if err := s.deliverTeamEventOutbox(team, b, missing); err == nil {
			t.Fatal("dispatch accepted a payload-supplied receipt identity")
		}
		if entries, err := b.XRevRange(ctx, destination, 1); err != nil || len(entries) != 0 {
			t.Fatalf("invalid dispatch wrote %d stream entries: %v", len(entries), err)
		}
	})
	positive := outbox(strconv.Quote(canonical), task.MessageID, destination)
	for range 2 {
		if err := s.deliverTeamEventOutbox(team, b, positive); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := b.XRevRange(ctx, destination, 2)
	if err != nil || len(entries) != 1 || entries[0].Fields["task_id"] != canonical {
		t.Fatalf("canonical dispatch wire identity or replay was changed: %v %v", entries, err)
	}
	saved, err := repo.GetTaskByID(task.ID)
	if err != nil || saved == nil || saved.Status != models.TeamTaskStatusDispatched || saved.RedisStreamID == nil || *saved.RedisStreamID != entries[0].ID {
		t.Fatalf("SQL dispatch did not use the Redis receipt: %+v %v", saved, err)
	}
}

func TestDeletedTeamRedisCleanupRetainsOnObjectFailureAndResumes(t *testing.T) {
	sess, repo, team, task, _ := projectionMySQL(t)
	b, _ := capacityRedisFixture(t)
	cleanupMaintenanceTeam(t, b, team.ID)
	ctx := context.Background()
	r := repo.(repository.TeamRedisMaintenanceRepository)
	if err := r.PrepareTeamRedisCleanup(team, []int{}); err != nil {
		t.Fatal(err)
	}
	members, err := repo.ListMembersByTeamID(team.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range members {
		members[i].Status = "deleted"
		if err = repo.UpdateMember(&members[i]); err != nil {
			t.Fatal(err)
		}
	}
	team.Status = "deleted"
	team.UpdatedAt = time.Now().Add(-time.Minute)
	if err = repo.UpdateTeam(team); err != nil {
		t.Fatal(err)
	}
	key := teamInboxKey(team.ID, "worker")
	if _, err = b.XAdd(ctx, key, map[string]string{"payload": `{"event":"work","body":"must survive"}`}); err != nil {
		t.Fatal(err)
	}
	objects := &maintenanceObjects{fail: true, values: map[string][]byte{}}
	master := []byte(strings.Repeat("k", 32))
	encrypted, err := observability.NewEncryptedObjects(objects, master)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := observability.NewJournal(t.TempDir(), master, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	restore := repository.ConfigureObservability(sess, journal, encrypted)
	defer restore()
	s := &teamService{repo: repo}
	if err = s.cleanupDeletedTeamRedis(ctx, r, team, b); err == nil {
		t.Fatal("object outage did not fence deletion")
	}
	if n, _ := b.doRaw(ctx, "XLEN", key); n != int64(1) {
		t.Fatal("unarchived accepted work deleted", n)
	}
	objects.fail = false
	for i := 0; i < 100; i++ {
		if _, err = sess.SQL().Exec("UPDATE team_redis_cleanup_jobs SET lease_until=NULL WHERE team_id=?", team.ID); err != nil {
			t.Fatal(err)
		}
		if err = s.cleanupDeletedTeamRedis(ctx, r, team, b); err != nil {
			t.Fatal(err)
		}
		job, e := r.TeamRedisCleanupJob(team.ID)
		if e != nil {
			t.Fatal(e)
		}
		if job.State == "complete" {
			break
		}
		if i == 99 {
			t.Fatal("cleanup failed to make durable progress")
		}
	}
	if n, _ := b.doRaw(ctx, "EXISTS", key); n != int64(0) {
		t.Fatal("archived Redis history not reclaimed", n)
	}
	got, err := repo.GetTaskByID(task.ID)
	if err != nil || got.Status != "cancelled" || got.WorkflowState != "failed" || got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "user-requested") {
		t.Fatal("unfinished work lacks explicit termination record", got, err)
	}
	rows, err := sess.SQL().Query("SELECT object_ref FROM team_redis_archive_receipts WHERE team_id=?", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if !json.Valid([]byte(raw)) {
			t.Fatal("invalid archive locator")
		}
		if _, err = repository.DurableArchiveRead(ctx, fmt.Sprintf("user:%d", team.UserID), json.RawMessage(raw)); err != nil {
			t.Fatal("archive not independently readable", err)
		}
		count++
	}
	if err = rows.Err(); err != nil || count == 0 {
		t.Fatal("missing archive evidence", count, err)
	}
}

func TestTeamReceiptClosureReclaimsAfterSourceExitAndNeverRepublishes(t *testing.T) {
	sess, repo, team, task, message := projectionMySQL(t)
	b, _ := capacityRedisFixture(t)
	cleanupMaintenanceTeam(t, b, team.ID)
	ctx := context.Background()
	s := &teamService{repo: repo}
	if !s.projectConsumedTeamEvent(ctx, team, nil, message) {
		t.Fatal("projection baseline failed")
	}
	old := time.Now().UTC().AddDate(0, 0, -9)
	task, err := repo.GetTaskByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.Status = "succeeded"
	task.FinishedAt = &old
	if err = repo.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("UPDATE team_work_items SET status='completed',finished_at=? WHERE root_task_id=?", old, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("UPDATE team_workflow_phases SET status='completed',completed_at=? WHERE root_task_id=?", old, task.ID); err != nil {
		t.Fatal(err)
	}
	key := teamInboxKey(team.ID, "worker")
	payload := fmt.Sprintf(`{"taskId":%d,"messageId":"retired"}`, task.ID)
	out := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: "dispatch:retired", Destination: key, MessageID: "retired", PayloadJSON: payload, Status: "delivered", CreatedAt: old, DeliveredAt: &old}
	if err = repo.CreateEventOutbox(out); err != nil {
		t.Fatal(err)
	}
	if _, err = b.doRaw(ctx, "XADD", key, "1-0", "payload", "old accepted source"); err != nil {
		t.Fatal(err)
	}
	receipt := teamCapacityPrefix(team.ID) + "receipt:" + repository.TeamOutboxReceiptIdentity(team.ID, key, out.MessageID)
	newID, err := b.xAddReliableOnce(ctx, key, map[string]string{"payload": payload}, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.reclaimTeamDeliveryReceipts(ctx, team, b); err == nil {
		t.Fatal("retained old source did not fence receipt reclamation")
	}
	if v, _ := b.doRaw(ctx, "GET", receipt); v != newID {
		t.Fatal("unsafe receipt deletion", v)
	}
	if _, err = b.do(ctx, "XGROUP", "CREATE", key, "required", "0", "MKSTREAM"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "XREADGROUP", "GROUP", "required", "worker", "COUNT", "10", "STREAMS", key, ">"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "XACK", key, "required", "1-0", newID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.trimReliableTeamStream(ctx, key, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.reclaimTeamDeliveryReceipts(ctx, team, b); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.doRaw(ctx, "GET", receipt); v != nil {
		t.Fatal("closed receipt was not reclaimed", v)
	}
	if _, err = sess.SQL().Exec("DELETE FROM team_event_outbox WHERE id=?", out.ID); err != nil {
		t.Fatal(err)
	}
	retry := &models.TeamEventOutbox{TeamID: out.TeamID, SourceEventID: out.SourceEventID, Destination: out.Destination, MessageID: out.MessageID, PayloadJSON: out.PayloadJSON}
	if err = repo.CreateEventOutbox(retry); err != nil || retry.Status != "delivered" {
		t.Fatal("retired obligation resurrected", retry, err)
	}
	if err = s.deliverTeamEventOutbox(team, b, retry); err != nil {
		t.Fatal(err)
	}
	if n, _ := b.doRaw(ctx, "XLEN", key); n != int64(0) {
		t.Fatal("retired sender republished after dedup removal", n)
	}
	business, completion, stream := "terminal-event", "completion:legacy", "2-0"
	event := &models.TeamEvent{TeamID: team.ID, TaskID: &task.ID, EventID: &business, CompletionID: &completion, RedisStreamID: &stream, EventType: "task_completed", CreatedAt: old}
	if err = repo.CreateEvent(event); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(business))
	canonical := fmt.Sprintf("claw:team:%d:completions:delivery-%x", team.ID, digest)
	legacy := fmt.Sprintf("claw:team:%d:completions:completion_legacy", team.ID)
	if _, err = b.do(ctx, "SET", canonical, stream); err != nil {
		t.Fatal(err)
	}
	if _, err = b.do(ctx, "SET", legacy, "different-active-event"); err != nil {
		t.Fatal(err)
	}
	if err = s.reclaimTeamDeliveryReceipts(ctx, team, b); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.doRaw(ctx, "GET", canonical); v != nil {
		t.Fatal("terminal delivery marker not reclaimed", v)
	}
	if v, _ := b.doRaw(ctx, "GET", legacy); v != "different-active-event" {
		t.Fatal("legacy collision deleted another identity", v)
	}
	if found, err := repo.EventExistsByCompletionID(team.ID, completion); err != nil || !found {
		t.Fatal("permanent SQL business identity was lost", found, err)
	}
}
