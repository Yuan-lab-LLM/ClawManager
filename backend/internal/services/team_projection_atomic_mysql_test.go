package services

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

func projectionMySQL(t *testing.T) (db.Session, repository.TeamRepository, *models.Team, *models.TeamTask, redisStreamMessage) {
	t.Helper()
	portText := os.Getenv("TEST_DATABASE_MYSQL_PORT")
	if portText == "" {
		t.Skip("disposable MySQL fixture required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid fixture port")
	}
	root, err := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:" + portText, User: "root", Database: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	row, err := root.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'")
	if err != nil {
		t.Fatal(err)
	}
	var marker int
	if err = row.Scan(&marker); err != nil || marker != 1 {
		root.Close()
		t.Fatal("fixture marker required", err)
	}
	name := fmt.Sprintf("cm_projection_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	sess, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name, MaxOpenConns: 8, MaxIdleConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	repo := repository.NewTeamRepository(sess)
	team := &models.Team{ID: int(time.Now().UnixNano()%1000000000) + 10000, UserID: 1, Name: "projection", Status: "running", CommunicationMode: "leader_mediated", RedisEventsLastID: "0-0"}
	if err = repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	member := &models.TeamMember{TeamID: team.ID, UserID: 1, MemberKey: "leader", DisplayName: "Leader", Role: "leader", RuntimeType: "opencode", InstanceMode: "lite", Status: "idle", Availability: "idle"}
	if err = repo.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	task := &models.TeamTask{TeamID: team.ID, TargetMemberID: member.ID, MessageID: "root", Status: "running", WorkflowState: "planning", LedgerVersion: 1, PayloadJSON: `{}`}
	if err = repo.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	message := redisStreamMessage{ID: "100-0", Fields: map[string]string{"payload": fmt.Sprintf(`{"eventId":"plan-event","event":"task_progress","eventKind":"leader_plan","memberId":"leader","taskId":%d,"planVersion":1,"phases":[{"phaseId":"phase-1","status":"active"}]}`, task.ID)}}
	return sess, repo, team, task, message
}

// Break the actual MySQL COMMIT exchange, once, either before the server sees
// it or after its OK packet. This exercises both rollback and unknown outcome.
func projectionCommitProxy(t *testing.T, afterCommit bool) (string, *atomic.Bool) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	armed := &atomic.Bool{}
	armed.Store(true)
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", "127.0.0.1:"+os.Getenv("TEST_DATABASE_MYSQL_PORT"))
			if err != nil {
				client.Close()
				continue
			}
			go func() {
				defer client.Close()
				defer server.Close()
				var dropReply atomic.Bool
				go func() {
					defer client.Close()
					defer server.Close()
					for {
						packet, err := readProjectionMySQLPacket(server)
						if err != nil {
							return
						}
						if dropReply.Load() {
							return
						}
						if _, err = client.Write(packet); err != nil {
							return
						}
					}
				}()
				for {
					packet, err := readProjectionMySQLPacket(client)
					if err != nil {
						return
					}
					commit := len(packet) > 5 && packet[4] == 3 && strings.EqualFold(strings.TrimSpace(string(packet[5:])), "COMMIT")
					if commit && armed.CompareAndSwap(true, false) {
						if !afterCommit {
							return
						}
						dropReply.Store(true)
					}
					if _, err = server.Write(packet); err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), armed
}

func readProjectionMySQLPacket(reader io.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint32(header) & 0xffffff)
	packet := make([]byte, 4+size)
	copy(packet, header)
	_, err := io.ReadFull(reader, packet[4:])
	return packet, err
}

func TestAtomicProjectionMySQLCommitConnectionFailure(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_server_commit=%t", after), func(t *testing.T) {
			sess, repo, team, task, message := projectionMySQL(t)
			row, err := sess.SQL().QueryRow("SELECT DATABASE()")
			if err != nil {
				t.Fatal(err)
			}
			var name string
			if err = row.Scan(&name); err != nil {
				t.Fatal(err)
			}
			address, armed := projectionCommitProxy(t, after)
			proxied, err := mysql.Open(mysql.ConnectionURL{Host: address, User: "root", Database: name, Options: map[string]string{"parseTime": "true"}})
			if err != nil {
				t.Fatal(err)
			}
			defer proxied.Close()
			service := &teamService{repo: repository.NewTeamRepository(proxied)}
			service.projectConsumedTeamEvent(context.Background(), team, nil, message)
			if armed.Load() {
				t.Fatal("projection did not reach an atomic COMMIT")
			}
			current, _ := repo.GetTaskByID(task.ID)
			want := int64(1)
			if after {
				want = 2
			}
			if current.LedgerVersion != want {
				t.Fatalf("unexpected commit outcome: %d want %d", current.LedgerVersion, want)
			}
			if !(&teamService{repo: repo}).projectConsumedTeamEvent(context.Background(), team, nil, message) {
				t.Fatal("restart after broken COMMIT failed")
			}
			current, _ = repo.GetTaskByID(task.ID)
			if current.LedgerVersion != 2 {
				t.Fatal("broken COMMIT retry repeated ledger", current.LedgerVersion)
			}
		})
	}
}

func TestAtomicProjectionMySQLDeliveredRetryCannotBecomePending(t *testing.T) {
	_, repo, team, _, _ := projectionMySQL(t)
	outbox := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: "retry", MessageID: "retry", Destination: teamProjectionRetryDestination, PayloadJSON: `{}`}
	if err := repo.CreateEventOutbox(outbox); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkEventOutboxDelivered(outbox.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkEventOutboxFailed(outbox.ID, time.Now().UTC(), "stale worker failed"); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListPendingEventOutbox(time.Now().UTC().Add(time.Hour), 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("stale failure reopened delivered replay: %+v %v", rows, err)
	}
}

func TestAtomicProjectionMySQLRollbackAndRestart(t *testing.T) {
	sess, repo, team, task, message := projectionMySQL(t)
	if _, err := sess.SQL().Exec(`CREATE TRIGGER fail_projection_member BEFORE UPDATE ON team_members FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected member failure'`); err != nil {
		t.Fatal(err)
	}
	service := &teamService{repo: repo}
	service.projectConsumedTeamEvent(context.Background(), team, nil, message)
	if exists, err := repo.EventExistsByStreamID(team.ID, message.ID); err != nil || exists {
		t.Fatalf("rolled-back source dedup must not survive: exists=%t err=%v", exists, err)
	}
	current, err := repo.GetTaskByID(task.ID)
	if err != nil || current.LedgerVersion != 1 {
		t.Fatalf("partial ledger survived rollback: %+v %v", current, err)
	}
	phases, err := repo.ListWorkflowPhasesByRootTaskID(task.ID)
	if err != nil || len(phases) != 0 {
		t.Fatalf("partial phases survived rollback: %+v %v", phases, err)
	}
	if _, err = sess.SQL().Exec("DROP TRIGGER fail_projection_member"); err != nil {
		t.Fatal(err)
	}
	restarted := &teamService{repo: repository.NewTeamRepository(sess)}
	if !restarted.projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("restart retry failed")
	}
	current, err = repo.GetTaskByID(task.ID)
	if err != nil || current.LedgerVersion != 2 {
		t.Fatalf("retry must apply ledger once: %+v %v", current, err)
	}
	message.ID = "101-0"
	if !restarted.projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("duplicate business event failed")
	}
	current, _ = repo.GetTaskByID(task.ID)
	if current.LedgerVersion != 2 {
		t.Fatal("duplicate business event incremented ledger", current.LedgerVersion)
	}
}

func TestAtomicProjectionMySQLCursorFailureRollsBackBusiness(t *testing.T) {
	sess, repo, team, task, message := projectionMySQL(t)
	if _, err := sess.SQL().Exec(`CREATE TRIGGER fail_projection_cursor BEFORE UPDATE ON teams FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected cursor failure'`); err != nil {
		t.Fatal(err)
	}
	if (&teamService{repo: repo}).projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("failed cursor acknowledged")
	}
	current, _ := repo.GetTaskByID(task.ID)
	if current.LedgerVersion != 1 {
		t.Fatal("cursor failure committed business ledger", current.LedgerVersion)
	}
	if exists, _ := repo.EventExistsByStreamID(team.ID, message.ID); exists {
		t.Fatal("cursor failure committed source dedup")
	}
	if team.RedisEventsLastID != "0-0" {
		t.Fatal("failed cursor changed memory")
	}
}

func TestAtomicProjectionMySQLConcurrentDelivery(t *testing.T) {
	sess, repo, team, task, message := projectionMySQL(t)
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := *team
			results <- (&teamService{repo: repository.NewTeamRepository(sess)}).projectConsumedTeamEvent(context.Background(), &copy, nil, message)
		}()
	}
	wg.Wait()
	close(results)
	for ok := range results {
		if !ok {
			t.Fatal("concurrent projection failed")
		}
	}
	current, _ := repo.GetTaskByID(task.ID)
	if current.LedgerVersion != 2 {
		t.Fatal("concurrent delivery applied ledger more than once", current.LedgerVersion)
	}
}

func TestAtomicProjectionMySQLConcurrentInlineAndReplay(t *testing.T) {
	sess, repo, team, task, message := projectionMySQL(t)
	encoded, _ := json.Marshal(message)
	id := boundedTeamEventID("projection-v2", team.ID, message.ID)
	retry := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: id, MessageID: id, Destination: teamProjectionRetryDestination, PayloadJSON: string(encoded)}
	if err := repo.CreateEventOutbox(retry); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(replay bool) {
			defer wg.Done()
			copy := *team
			s := &teamService{repo: repository.NewTeamRepository(sess)}
			if replay {
				failures <- s.projectTeamEventAtomic(context.Background(), &copy, nil, message, false, retry)
			} else {
				if !s.projectConsumedTeamEvent(context.Background(), &copy, nil, message) {
					failures <- fmt.Errorf("inline failed")
				}
			}
		}(i%2 == 0)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, _ := repo.GetTaskByID(task.ID)
	if current.LedgerVersion != 2 {
		t.Fatal("concurrent replay repeated ledger", current.LedgerVersion)
	}
}

func projectionRedis(t *testing.T, teamID int) *redisBus {
	t.Helper()
	raw := os.Getenv("TEST_TEAM_CONTEXT_REDIS_URL")
	if raw == "" {
		t.Skip("disposable Redis required")
	}
	if !strings.HasPrefix(raw, "redis://127.0.0.1:") {
		t.Fatal("loopback Redis required")
	}
	bus, err := newRedisBus(raw)
	if err != nil {
		t.Fatal(err)
	}
	marker, ok, err := bus.Get(context.Background(), "cm:test:runtime-event-retention:isolated")
	if err != nil || !ok || marker != "cm-redis-retention-test-20261104" {
		t.Fatal("isolated Redis marker required")
	}
	prefix := fmt.Sprintf("claw:team:%d:", teamID)
	t.Cleanup(func() {
		cursor := "0"
		for {
			reply, err := bus.doRaw(context.Background(), "SCAN", cursor, "MATCH", prefix+"*", "COUNT", "200")
			if err != nil {
				return
			}
			parts := reply.([]interface{})
			cursor = parts[0].(string)
			for _, key := range parts[1].([]interface{}) {
				bus.doRaw(context.Background(), "UNLINK", key.(string))
			}
			if cursor == "0" {
				return
			}
		}
	})
	return bus
}

func TestAtomicProjectionMySQLRedisEffectsWaitForCommitAndSurviveRestart(t *testing.T) {
	sess, repo, team, task, message := projectionMySQL(t)
	bus := projectionRedis(t, team.ID)
	message.Fields["payload"] = fmt.Sprintf(`{"eventId":"recover-event","event":"task_progress","eventKind":"turn_finished_without_completion","activeTurnFinished":true,"memberId":"leader","taskId":%d}`, task.ID)
	if _, err := sess.SQL().Exec(`CREATE TRIGGER fail_projection_cursor BEFORE UPDATE ON teams FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected cursor failure'`); err != nil {
		t.Fatal(err)
	}
	if (&teamService{repo: repo}).projectConsumedTeamEvent(context.Background(), team, bus, message) {
		t.Fatal("cursor failure acknowledged")
	}
	key := teamInboxKey(team.ID, "leader")
	length, err := bus.doRaw(context.Background(), "XLEN", key)
	if err != nil || length != int64(0) {
		t.Fatal("Redis side effect escaped rollback", length, err)
	}
	if _, err = sess.SQL().Exec("DROP TRIGGER fail_projection_cursor"); err != nil {
		t.Fatal(err)
	}
	if !(&teamService{repo: repo}).projectConsumedTeamEvent(context.Background(), team, bus, message) {
		t.Fatal("projection failed")
	}
	length, _ = bus.doRaw(context.Background(), "XLEN", key)
	if length != int64(0) {
		t.Fatal("outbound append ran before postcommit worker")
	}
	rows, err := repo.ListPendingEventOutbox(time.Now().UTC().Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &teamService{repo: repository.NewTeamRepository(sess)}
	commands := 0
	for _, outbox := range rows {
		if outbox.Destination == teamProjectionCommandDestination {
			commands++
		}
		for i := 0; i < 2; i++ {
			if err := restarted.deliverTeamEventOutbox(team, bus, &outbox); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.MarkEventOutboxDelivered(outbox.ID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if commands == 0 {
		t.Fatal("no durable commands survived restart")
	}
	length, _ = bus.doRaw(context.Background(), "XLEN", key)
	if length != int64(1) {
		t.Fatal("postcommit retry duplicated or lost append", length)
	}
}

func TestAtomicProjectionMySQLWatermarkRequiresResolvedHistory(t *testing.T) {
	sess, repo, team, _, message := projectionMySQL(t)
	proof := repo.(repository.TeamProjectionRepository)
	if _, err := proof.GetTeamProjectionWatermark(context.Background(), team.ID); err == nil {
		t.Fatal("unproved watermark exposed")
	}
	if _, err := sess.SQL().Exec(`CREATE TRIGGER fail_projection_member BEFORE UPDATE ON team_members FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='member failure'`); err != nil {
		t.Fatal(err)
	}
	if !(&teamService{repo: repo}).projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("durable handoff failed")
	}
	if _, err := proof.GetTeamProjectionWatermark(context.Background(), team.ID); err == nil {
		t.Fatal("pending replay authorized trim")
	}
	if _, err := sess.SQL().Exec("DROP TRIGGER fail_projection_member"); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListPendingEventOutbox(time.Now().UTC().Add(time.Hour), 100)
	if err != nil || len(rows) != 1 {
		t.Fatal("missing replay", err)
	}
	if err := (&teamService{repo: repo}).projectTeamEventAtomic(context.Background(), team, nil, message, false, &rows[0]); err != nil {
		t.Fatal(err)
	}
	if cursor, err := proof.GetTeamProjectionWatermark(context.Background(), team.ID); err != nil || cursor != message.ID {
		t.Fatal("committed resolved watermark missing", cursor, err)
	}
}

func TestAtomicProjectionMySQLLegacyPartialReplayFailsClosed(t *testing.T) {
	_, repo, team, task, message := projectionMySQL(t)
	encoded, _ := json.Marshal(message)
	id := boundedTeamEventID("projection", team.ID, message.ID)
	retry := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: id, MessageID: id, Destination: teamProjectionRetryDestination, PayloadJSON: string(encoded)}
	if err := repo.CreateEventOutbox(retry); err != nil {
		t.Fatal(err)
	}
	stream := message.ID
	eventID := "plan-event"
	if err := repo.CreateEvent(&models.TeamEvent{TeamID: team.ID, RedisStreamID: &stream, EventID: &eventID, EventType: "task_progress"}); err != nil {
		t.Fatal(err)
	}
	if err := (&teamService{repo: repo}).projectTeamEventAtomic(context.Background(), team, nil, message, true, nil); err == nil {
		t.Fatal("inline duplicate certified unknown legacy writes")
	}
	current, _ := repo.GetTaskByID(task.ID)
	if current.LedgerVersion != 1 {
		t.Fatal("ambiguous replay changed ledger")
	}
}

func TestAtomicProjectionMySQLLegacyAuditCapability(t *testing.T) {
	_, repo, team, task, message := projectionMySQL(t)
	audit, ok := repo.(interface {
		ExportTeamProjectionAudit(context.Context, int) (json.RawMessage, error)
		ApplyTeamProjectionAudit(context.Context, json.RawMessage) error
	})
	if !ok {
		t.Fatal("legacy projection audit export/apply capability missing")
	}
	team.RedisEventsLastID = "100-0"
	if err := repo.UpdateTeam(team); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(message)
	id := boundedTeamEventID("projection", team.ID, message.ID)
	retry := &models.TeamEventOutbox{TeamID: team.ID, SourceEventID: id, MessageID: id, Destination: teamProjectionRetryDestination, PayloadJSON: string(encoded)}
	if err := repo.CreateEventOutbox(retry); err != nil {
		t.Fatal(err)
	}
	draft, err := audit.ExportTeamProjectionAudit(context.Background(), team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyTeamProjectionAudit(context.Background(), draft); err == nil {
		t.Fatal("unreviewed manifest applied")
	}
	approve := func(raw json.RawMessage) json.RawMessage {
		var manifest map[string]interface{}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest["reviewer"] = "fixture-reviewer"
		manifest["baseline_evidence"] = "fixture://reviewed-full-business-snapshot-and-replay-source-window"
		rows := manifest["resolutions"].([]interface{})
		for _, row := range rows {
			v := row.(map[string]interface{})
			v["disposition"] = "effects_verified_complete"
			v["evidence"] = "fixture://verified-ledger-work-items-and-all-outbound-obligations"
		}
		result, _ := json.Marshal(manifest)
		return result
	}
	approved := approve(draft)
	task.LedgerVersion++
	if err := repo.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyTeamProjectionAudit(context.Background(), approved); err == nil {
		t.Fatal("stale reviewed snapshot applied")
	}
	draft, err = audit.ExportTeamProjectionAudit(context.Background(), team.ID)
	if err != nil {
		t.Fatal(err)
	}
	approved = approve(draft)
	if err := audit.ApplyTeamProjectionAudit(context.Background(), approved); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyTeamProjectionAudit(context.Background(), approved); err != nil {
		t.Fatal("audit apply retry not idempotent", err)
	}
	if cursor, err := repo.(repository.TeamProjectionRepository).GetTeamProjectionWatermark(context.Background(), team.ID); err != nil || cursor != "100-0" {
		t.Fatal("reviewed baseline not established", cursor, err)
	}
	if !(&teamService{repo: repo}).projectConsumedTeamEvent(context.Background(), team, nil, message) {
		t.Fatal("reviewed duplicate retry failed")
	}
	current, _ := repo.GetTaskByID(task.ID)
	if current.LedgerVersion != 2 {
		t.Fatal("reviewed completed legacy projection repeated ledger", current.LedgerVersion)
	}
}
