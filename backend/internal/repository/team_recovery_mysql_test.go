package repository_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/upper/db/v4/adapter/mysql"
)

func TestRecoveryObservationAndOutboxAreAtomicMySQL(t *testing.T) {
	portText := os.Getenv("TEST_TEAM_RECOVERY_MYSQL_PORT")
	if portText == "" {
		t.Skip("disposable localhost MySQL required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid localhost port")
	}
	root, err := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:" + portText, User: "root", Database: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("team_recovery_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.SQL().Exec("DROP DATABASE " + name); _ = root.Close() })
	s, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name, MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	repo := repository.NewTeamRepository(s)
	team := &models.Team{UserID: 1, Name: "atomic recovery", Status: "running", CommunicationMode: "leader_mediated", RedisEventsLastID: "0-0"}
	if err = repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	sourceID, recoveryID, stream := "source-turn", "recovery-turn", "1-0"
	source := &models.TeamEvent{TeamID: team.ID, EventID: &sourceID, RedisStreamID: &stream, EventType: "task_progress"}
	recovery := &models.TeamEvent{TeamID: team.ID, EventID: &recoveryID, EventType: "root_assignment_recovery_requested"}
	outbox := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: recoveryID, MessageID: recoveryID, Destination: strings.Repeat("x", 4096), PayloadJSON: `{}`, Status: "pending"}
	if repo.CreateEventWithOutbox(recovery, outbox, source) == nil {
		t.Fatal("expected real outbox insert failure")
	}
	for _, table := range []string{"team_events", "team_event_outbox"} {
		n, e := s.Collection(table).Find().Count()
		if e != nil || n != 0 {
			t.Fatalf("partial commit %s: %d/%v", table, n, e)
		}
	}
	// Retry with fresh in-memory objects, as a real stream replay does.
	source.ID = 0
	source.SequenceNo = 0
	recovery.ID = 0
	recovery.SequenceNo = 0
	outbox.ID = 0
	outbox.Destination = "claw:team:test:inbox:worker"
	if err = repo.CreateEventWithOutbox(recovery, outbox, source); err != nil {
		t.Fatal(err)
	}
	recovery.ID = 0
	outbox.ID = 0
	if err = repo.CreateEventWithOutbox(recovery, outbox, source); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]uint64{"team_events": 2, "team_event_outbox": 1} {
		n, e := s.Collection(table).Find().Count()
		if e != nil || n != want {
			t.Fatalf("replay count %s: %d/%v", table, n, e)
		}
	}
	t.Run("delivery transaction and strict event IDs", func(t *testing.T) {
		member := &models.TeamMember{TeamID: team.ID, UserID: 1, MemberKey: "worker", DisplayName: "Worker", Role: "worker", RuntimeType: "opencode", InstanceMode: "lite", Status: "idle", Availability: "idle"}
		if e := repo.CreateMember(member); e != nil {
			t.Fatal(e)
		}
		task := &models.TeamTask{TeamID: team.ID, TargetMemberID: member.ID, MessageID: "atomic-delivery", Status: "running", WorkflowState: "executing", LedgerVersion: 1, PayloadJSON: `{}`}
		if e := repo.CreateTask(task); e != nil {
			t.Fatal(e)
		}
		now := time.Now().UTC()
		old := `{"completionId":"receipt","summary":"old"}`
		item := &models.TeamWorkItem{TeamID: team.ID, RootTaskID: task.ID, OwnerMemberID: &member.ID, WorkID: "work", Status: "succeeded", RequiredForRoot: true, ReviewRequired: true, ValidatedRevision: func() *int { v := 1; return &v }(), Revision: 1, ResultJSON: &old, FinishedAt: &now}
		if e := repo.UpsertWorkItem(item); e != nil {
			t.Fatal(e)
		}
		committer := repo.(interface {
			CommitTeamDelivery(*models.TeamWorkItem, *models.TeamEvent, *models.TeamEventOutbox, *models.TeamTask, int64, []*models.TeamEvent, ...*models.TeamEventOutbox) error
		})
		corrected := *item
		body := `{"completionId":"receipt","summary":"actual correction"}`
		corrected.ResultJSON = &body
		changed := *task
		changed.LedgerVersion = 2
		long := strings.Repeat("e", 139)
		event := &models.TeamEvent{TeamID: team.ID, TaskID: &task.ID, EventID: &long, EventType: "member_result_updated", PayloadJSON: &body}
		ack := &models.TeamEventOutbox{TeamID: team.ID, MessageID: "delivery-ack", SourceEventID: "delivery-update", Destination: strings.Repeat("d", 4096), PayloadJSON: `{}`}
		if committer.CommitTeamDelivery(&corrected, event, ack, &changed, 1, nil) == nil {
			t.Fatal("oversized event identity accepted")
		}
		short := "member-result-updated:" + strings.Repeat("f", 64)
		event.EventID = &short
		event.ID = 0
		if committer.CommitTeamDelivery(&corrected, event, ack, &changed, 1, nil) == nil {
			t.Fatal("outbox failure not reproduced")
		}
		current, e := repo.GetTaskByID(task.ID)
		if e != nil || current.LedgerVersion != 1 {
			t.Fatal("partial ledger committed", e)
		}
		items, e := repo.ListWorkItemsByRootTaskID(task.ID)
		if e != nil || items[0].ResultJSON == nil || *items[0].ResultJSON != old {
			t.Fatal("partial correction committed", e)
		}
		event.ID = 0
		ack.ID = 0
		ack.Destination = "claw:team:test:completion-ack"
		if e = committer.CommitTeamDelivery(&corrected, event, ack, &changed, 1, nil); e != nil {
			t.Fatal(e)
		}
		current, e = repo.GetTaskByID(task.ID)
		if e != nil || current.LedgerVersion != 2 {
			t.Fatal("correction did not commit", e)
		}
		items, e = repo.ListWorkItemsByRootTaskID(task.ID)
		if e != nil || items[0].ResultJSON == nil || *items[0].ResultJSON != body || items[0].ValidatedRevision != nil {
			t.Fatal("correction/result review boundary not committed", e)
		}
		if committer.CommitTeamDelivery(&corrected, event, ack, &changed, 1, nil) == nil {
			t.Fatal("stale ledger overwrote result")
		}
		for i := 0; i < 3; i++ {
			x := *task
			x.ID = 0
			x.MessageID = fmt.Sprintf("page-%d", i)
			if e = repo.CreateTask(&x); e != nil {
				t.Fatal(e)
			}
		}
		pager := repo.(interface {
			ListActiveTeamTasksAfterID(int, int) ([]models.TeamTask, error)
		})
		after, count := 0, 0
		for {
			page, e := pager.ListActiveTeamTasksAfterID(after, 1)
			if e != nil {
				t.Fatal(e)
			}
			if len(page) == 0 {
				break
			}
			count++
			after = page[0].ID
		}
		if count != 4 {
			t.Fatalf("active task pages lost: %d", count)
		}
	})

}
