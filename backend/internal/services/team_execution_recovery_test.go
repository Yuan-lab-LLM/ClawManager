package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRootDependencyObservationCannotHideStalledOrCompletedWork(t *testing.T) {
	now := time.Now().UTC()
	task := &models.TeamTask{ID: 3, TargetMemberID: 1}
	item := models.TeamWorkItem{RootTaskID: 3, OwnerMemberID: intPtr(2), Status: "running"}
	snapshot := &teamAssignmentActivitySnapshot{TurnState: "running", ObservedAt: now.Format(time.RFC3339Nano)}
	if !progressingRootDependency(task, item, snapshot, now) {
		t.Fatal("known active dependency not recognized")
	}
	for _, state := range []string{"succeeded", "failed", "cancelled", "waiting"} {
		copy := item
		copy.Status = state
		if progressingRootDependency(task, copy, snapshot, now) {
			t.Fatalf("%s hid a stalled root", state)
		}
	}
	snapshot.StallCandidate = true
	if progressingRootDependency(task, item, snapshot, now) {
		t.Fatal("stalled worker hid root stall")
	}
	snapshot.StallCandidate = false
	snapshot.ObservedAt = now.Add(-time.Hour).Format(time.RFC3339Nano)
	if progressingRootDependency(task, item, snapshot, now) {
		t.Fatal("old heartbeat hid root stall")
	}
}

func TestDeliveryEventIDFitsRealSchemaAndSeparatesCorrections(t *testing.T) {
	a := boundedTeamEventID("member-result-updated", 170, 371, "worker", strings.Repeat("assignment", 40), 1, "sha256:"+strings.Repeat("f", 64))
	b := boundedTeamEventID("member-result-updated", 170, 371, "worker", strings.Repeat("assignment", 40), 1, "sha256:"+strings.Repeat("e", 64))
	if len(a) > 128 || a == b {
		t.Fatal("unbounded or colliding result update identity")
	}
}

func TestNativeSupplementPreservesReceiptAndCannotCloseOrReopenRoot(t *testing.T) {
	for _, status := range []string{"running", "succeeded"} {
		for _, nativeRuntime := range []string{"opencode", "deepseek-harness"} {
			t.Run(status, func(t *testing.T) {
				now := time.Now().UTC()
				task := &models.TeamTask{ID: 3, TeamID: 12, TargetMemberID: 1, MessageID: "root", Status: status, LedgerVersion: 4}
				member := &models.TeamMember{ID: 2, TeamID: 12, MemberKey: "worker", RuntimeType: nativeRuntime}
				original := `{"completionId":"receipt","sourceMessageId":"original-input","summary":"done","resultMarkdown":"done","contentHash":"original-hash"}`
				repo := &teamRepositoryStub{workItems: []models.TeamWorkItem{{ID: 4, TeamID: 12, RootTaskID: 3, WorkID: "work", AssignmentID: stringPtr("work"), Revision: 1, OwnerMemberID: intPtr(2), Status: "succeeded", FinishedAt: &now, ResultJSON: &original}}}
				s := &teamService{repo: repo}
				team := &models.Team{ID: 12}
				payload := map[string]interface{}{"nativePublicDelivery": true, "sourceRecordId": "msg_answer", "nativeInputId": "msg_input", "sourceMessageId": "original-input", "assignmentId": "work", "revision": 1, "text": "Actual full public answer", "eventKind": "delivery_supplement", "eventId": "supplement", "nonAuthoritative": true, "stateEffect": "none", "visibleToChat": true}
				if err := s.projectNativeDeliverySupplement(team, nil, task, member, payload, "20-0"); err != nil {
					t.Fatal(err)
				}
				if task.Status != status || len(repo.workItems) != 1 {
					t.Fatal("supplement changed workflow")
				}
				result := workItemResultPayload(repo.workItems[0])
				if eventString(result, "contentHash") != "original-hash" || eventString(result, "resultMarkdown") != "done" {
					t.Fatal("formal receipt overwritten")
				}
				c := acknowledgedWorkItemContext(repo.workItems[0])
				if c["supplementaryText"] != "Actual full public answer" {
					t.Fatal("Leader cannot read supplement")
				}
				if len(teamEventPayloads(repo.createdEvents)) != 1 {
					t.Fatal("public delivery omitted from chat")
				}
				before := len(repo.createdEvents)
				payload["sourceMessageId"] = "another-input"
				if err := s.projectNativeDeliverySupplement(team, nil, task, member, payload, "21-0"); err != nil {
					t.Fatal(err)
				}
				if len(repo.createdEvents) != before {
					t.Fatal("foreign native turn was accepted")
				}
			})
		}
	}
}

type failedProjectionRepository struct {
	*teamRepositoryStub
	fail bool
}

func (r *failedProjectionRepository) EventExistsByStreamID(int, string) (bool, error) {
	return false, errors.New("temporary database failure")
}
func (r *failedProjectionRepository) CreateEventOutbox(o *models.TeamEventOutbox) error {
	if r.fail {
		return errors.New("outbox unavailable")
	}
	r.outboxRows = append(r.outboxRows, *o)
	return nil
}
func TestFailedProjectionAdvancesOnlyAfterDurableReplayIntent(t *testing.T) {
	r := &failedProjectionRepository{teamRepositoryStub: &teamRepositoryStub{}}
	s := &teamService{repo: r}
	team := &models.Team{ID: 12, RedisEventsLastID: "1-0"}
	m := redisStreamMessage{ID: "2-0", Fields: map[string]string{"payload": "{}"}}
	r.fail = true
	if s.projectConsumedTeamEvent(context.Background(), team, nil, m) || team.RedisEventsLastID != "1-0" {
		t.Fatal("lost projection was acknowledged")
	}
	r.fail = false
	if !s.projectConsumedTeamEvent(context.Background(), team, nil, m) || team.RedisEventsLastID != "2-0" || len(r.outboxRows) != 1 {
		t.Fatal("replay intent missing")
	}
	var replay redisStreamMessage
	if json.Unmarshal([]byte(r.outboxRows[0].PayloadJSON), &replay) != nil || replay.ID != m.ID {
		t.Fatal("original transport identity lost")
	}
}

func TestRootFailureWithoutWorkerIsObservedBeforeThirtyMinuteFallback(t *testing.T) {
	now := time.Now().UTC()
	task := &models.TeamTask{ID: 3, TeamID: 12, TargetMemberID: 1, MessageID: "root", Status: "running", CreatedAt: now.Add(-4 * time.Minute)}
	member := &models.TeamMember{ID: 1, TeamID: 12, MemberKey: "leader", Role: "leader", Status: "busy"}
	failure := `{"eventKind":"runtime_reconciliation_needed","retryable":true,"executionStopped":false}`
	repo := &teamRepositoryStub{teamsByID: map[int]*models.Team{12: {ID: 12, Status: "running"}}, membersByID: map[int]*models.TeamMember{1: member}, createdEvents: []models.TeamEvent{{ID: 7, TeamID: 12, TaskID: intPtr(3), MemberID: intPtr(1), PayloadJSON: &failure, CreatedAt: now.Add(-4 * time.Minute)}}}
	s := &teamService{repo: repo}
	if err := s.recoverRootExecution(task, now); err != nil {
		t.Fatal(err)
	}
	if len(repo.createdEvents) != 2 || !eventBool(teamEventPayloadMap(repo.createdEvents[1]), "recoveryBlocked") {
		t.Fatal("Leader root with no Worker was not observed")
	}
	if len(repo.outboxRows) != 0 || len(repo.workItems) != 0 {
		t.Fatal("unknown live execution triggered another model/assignment")
	}
	if err := s.recoverRootExecution(task, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(repo.createdEvents) != 2 {
		t.Fatal("unchanged blocked warning spammed")
	}
}

func TestStoppedLeaderRecoveryIsShortBoundedAndKeepsOriginalRoot(t *testing.T) {
	url := os.Getenv("TEST_TEAM_CONTEXT_REDIS_URL")
	if url == "" {
		t.Skip("disposable loopback Redis required")
	}
	if !strings.HasPrefix(url, "redis://127.0.0.1:") {
		t.Fatal("only disposable loopback Redis allowed")
	}
	t.Setenv("CLAWMANAGER_TEAM_REDIS_URL", url)
	now := time.Now().UTC()
	task := &models.TeamTask{ID: 33, TeamID: 512, TargetMemberID: 1, MessageID: "original-root", Status: "running", CreatedAt: now}
	member := &models.TeamMember{ID: 1, TeamID: 512, MemberKey: "leader", Role: "leader", Status: "busy"}
	failure := `{"eventKind":"runtime_reconciliation_needed","retryable":true,"executionStopped":true,"recoveryFaultKey":"fault-one"}`
	repo := &teamRepositoryStub{teamsByID: map[int]*models.Team{512: {ID: 512, Status: "running"}}, membersByID: map[int]*models.TeamMember{1: member}, createdEvents: []models.TeamEvent{{ID: 1, TeamID: 512, TaskID: intPtr(33), MemberID: intPtr(1), PayloadJSON: &failure, CreatedAt: now}}}
	service := &teamService{repo: repo}
	for i := 0; i < 3; i++ {
		if err := service.recoverRootExecution(task, now.Add(time.Duration(i)*2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if len(repo.outboxRows) != i+1 {
			t.Fatalf("missing bounded attempt %d", i+1)
		}
		for idx := range repo.createdEvents {
			p := teamEventPayloadMap(repo.createdEvents[idx])
			if eventInt(p, "boundedRecoveryAttempt") == i+1 {
				repo.createdEvents[idx].CreatedAt = now.Add(time.Duration(i) * 2 * time.Minute)
			}
		}
		var envelope map[string]interface{}
		if err := json.Unmarshal([]byte(repo.outboxRows[i].PayloadJSON), &envelope); err != nil {
			t.Fatal(err)
		}
		if eventString(envelope, "taskId") != "team-512-task-33" || eventBool(envelope, "requiresCompletion") || eventString(envelope, "intent") != "root_coordination_recovery" {
			t.Fatal("recovery changed dispatch contract")
		}
		if err := service.recoverRootExecution(task, now.Add(time.Duration(i)*2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if len(repo.outboxRows) != i+1 {
			t.Fatal("duplicate recovery admitted")
		}
		// A new attempt requires an actual failed native delivery, not elapsed
		// time while the preceding reminder is still queued.
		failurePayload, _ := marshalOptionalJSON(map[string]interface{}{"eventKind": "runtime_reconciliation_needed", "retryable": true, "executionStopped": true, "recoveryFaultKey": "fault-one", "boundedRecoveryAttempt": i + 1})
		repo.createdEvents = append(repo.createdEvents, models.TeamEvent{ID: len(repo.createdEvents) + 1, TeamID: 512, TaskID: intPtr(33), MemberID: intPtr(1), PayloadJSON: failurePayload, CreatedAt: now.Add(time.Duration(i)*2*time.Minute + time.Second)})
	}
	if err := service.recoverRootExecution(task, now.Add(8*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(repo.outboxRows) != 3 || len(repo.workItems) != 0 || task.Status != "running" {
		t.Fatal("recovery created work or falsely completed root")
	}
	blocked := false
	for _, e := range repo.createdEvents {
		blocked = blocked || eventBool(teamEventPayloadMap(e), "recoveryBlocked")
	}
	if !blocked {
		t.Fatal("bounded recovery exhaustion remained invisible")
	}
	bus, _ := newRedisBus(url)
	defer bus.Del(context.Background(), teamInboxKey(512, "leader"))
}

type supervisedRootRepository struct{ *teamRepositoryStub }

func (r *supervisedRootRepository) ListActiveTeamTasksAfterID(int, int) ([]models.TeamTask, error) {
	return nil, nil
}
func (r *supervisedRootRepository) ListRootMonitorEvents(taskID, limit int) ([]models.TeamEvent, error) {
	return r.ListEventsByTeamID(12, limit)
}
func (r *supervisedRootRepository) ListMonitorWorkItems(teamID int) ([]models.TeamWorkItem, error) {
	return r.ListWorkItemsByTeamID(teamID, 500)
}
func TestLegacyTimeoutCannotBypassUnknownNativeExecution(t *testing.T) {
	now := time.Now().UTC()
	task := &models.TeamTask{ID: 3, TeamID: 12, TargetMemberID: 1, Status: "running", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	p := `{"eventKind":"runtime_reconciliation_needed","retryable":true,"executionStopped":false}`
	r := &supervisedRootRepository{&teamRepositoryStub{teamsByID: map[int]*models.Team{12: {ID: 12, Status: "running"}}, membersByID: map[int]*models.TeamMember{1: {ID: 1, TeamID: 12, MemberKey: "leader", Role: "leader", Status: "busy"}}, createdEvents: []models.TeamEvent{{ID: 1, TeamID: 12, TaskID: intPtr(3), MemberID: intPtr(1), PayloadJSON: &p, CreatedAt: now.Add(-time.Hour)}}}}
	s := &teamService{repo: r}
	if e := s.observeTaskStall(task, 30*time.Minute); e != nil {
		t.Fatal(e)
	}
	if len(r.outboxRows) != 0 || task.Status != "running" {
		t.Fatal("legacy fallback admitted an unconfirmed competing execution")
	}
}

func TestLeaderNewRootCannotBeClearedByHistoricalCards(t *testing.T) {
	for _, status := range []string{"succeeded", "running", "waiting"} {
		leader := &models.TeamMember{ID: 1, Role: "leader", CurrentTaskID: intPtr(372), Status: "busy", Availability: "busy"}
		items := []models.TeamWorkItem{{OwnerMemberID: intPtr(1), RootTaskID: 371, Status: status, WorkID: "leader-final-synthesis"}}
		if reconcileTeamMemberOperationalState(leader, items) || leader.CurrentTaskID == nil || *leader.CurrentTaskID != 372 || leader.Status != "busy" {
			t.Fatal("historical card replaced active Leader root", status)
		}
	}
}

func TestLeaderNewRootReconcilesIdleAvailabilityWithoutReleasingOwnership(t *testing.T) {
	leader := &models.TeamMember{ID: 1, Role: "leader", CurrentTaskID: intPtr(373), Status: "busy", Availability: "idle"}
	items := []models.TeamWorkItem{{OwnerMemberID: intPtr(1), RootTaskID: 372, Status: "succeeded", WorkID: "leader-final-synthesis"}}
	if !reconcileTeamMemberOperationalState(leader, items) || leader.Availability != "busy" || leader.CurrentTaskID == nil || *leader.CurrentTaskID != 373 {
		t.Fatal("active Leader availability stayed idle")
	}
}
