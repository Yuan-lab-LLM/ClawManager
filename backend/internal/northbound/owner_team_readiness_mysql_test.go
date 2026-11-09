package northbound

import (
	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"context"
	"encoding/json"
	"testing"
	"time"
)

type waitingOwnerTeamEngine struct {
	services.TeamService
	ready bool
}

func (e *waitingOwnerTeamEngine) CheckCMTeam(int, int) error { return nil }
func (e *waitingOwnerTeamEngine) ProvisionOwnerTeam(context.Context, *models.Team, services.CreateTeamRequest, string, *repository.OwnerTeamRepository) error {
	if !e.ready {
		return &services.OwnerTeamRuntimePending{InstanceID: 553}
	}
	return nil
}

func TestOwnerTeamMySQLReadinessRequeuesWithoutAttemptAndTimesOut(t *testing.T) {
	sess := ownerTeamMySQL(t)
	ctx := context.Background()
	repo := repository.NewNorthboundRepository(sess)
	owner := "ready@example.com"
	team := &models.Team{UserID: 1, Name: "nb-readiness", Owner: &owner, ResourceScope: models.TeamScopeNorthbound, Status: "creating", CommunicationMode: "leader_mediated", SharedMountPath: "/team", RedisEventsLastID: "0-0"}
	if err := repository.NewTeamRepository(sess).CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	engine := &waitingOwnerTeamEngine{}
	core := NewCoreService(repo, repository.NewUserRepository(sess), nil, nil, config.NorthboundConfig{})
	core.OwnerTeams = NewOwnerTeamAPI(sess, engine, nil, nil, nil, true, 1)
	body, _ := json.Marshal(ownerTeamStoredRequest{Action: "teams.create", TeamRequest: &services.CreateTeamRequest{Name: "ready"}})
	now := time.Now().UTC()
	op := &models.NorthboundOperation{OperationID: "readiness-fixture", UserID: 1, Owner: &owner, TeamID: &team.ID, SessionID: "iei-ready", OperationType: ownerTeamOperationPrefix + "create", IdempotencyKeyHash: "ready", RequestHash: "ready", RequestPayload: string(body), Status: "queued", AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateOperation(op); err != nil {
		t.Fatal(err)
	}
	worker := NewOperationWorker(core, "fixture")
	read := func() models.NorthboundOperation {
		t.Helper()
		var item models.NorthboundOperation
		if err := sess.Collection("northbound_operations").Find("operation_id", op.OperationID).One(&item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	worker.processOne(ctx)
	one := read()
	if one.Status != "queued" || one.AttemptCount != 1 || one.ErrorCode == nil || *one.ErrorCode != "OWNER_TEAM_RUNTIME_WAITING" || one.LeaseOwner != nil {
		t.Fatalf("waiting observation lost: %+v", one)
	}
	if err := repo.DeferOwnerTeamReadiness(ctx, &one, "stale"); err == nil {
		t.Fatal("unowned lease requeued")
	}
	// Simulate process restart and passage of the durable backoff, not a new create.
	if _, err := sess.SQL().Exec(`UPDATE northbound_operations SET available_at=? WHERE operation_id=?`, now, op.OperationID); err != nil {
		t.Fatal(err)
	}
	worker = NewOperationWorker(core, "replacement")
	worker.processOne(ctx)
	two := read()
	if two.AttemptCount != 1 || two.StartedAt == nil || !two.StartedAt.Equal(*one.StartedAt) {
		t.Fatal("observation consumed attempt/reset deadline")
	}
	if _, err := sess.SQL().Exec(`UPDATE northbound_operations SET available_at=?,started_at=? WHERE operation_id=?`, now, now.Add(-16*time.Minute), op.OperationID); err != nil {
		t.Fatal(err)
	}
	worker.processOne(ctx)
	failed := read()
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "OWNER_TEAM_RUNTIME_TIMEOUT" {
		t.Fatalf("not bounded: %+v", failed)
	}
	store := repository.NewOwnerTeamRepository(sess)
	if ok, err := store.RetryOperation(&failed, failed.AttemptCount); err != nil || !ok {
		t.Fatal("retry denied", err)
	}
	engine.ready = true
	worker.processOne(ctx)
	finished := read()
	if finished.Status != "succeeded" || finished.AttemptCount != 2 {
		t.Fatal("retry did not finish original operation")
	}
	if ok, _ := store.RetryOperation(&finished, finished.AttemptCount); ok {
		t.Fatal("terminal operation reopened")
	}
}
