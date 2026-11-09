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
}
