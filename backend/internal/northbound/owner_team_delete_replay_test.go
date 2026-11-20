package northbound

import (
	"encoding/json"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestOwnerTeamDeletedOperationReplayMySQL(t *testing.T) {
	sess := ownerTeamMySQL(t)
	repo := repository.NewTeamRepository(sess)
	owner := "delete-replay@example.com"
	team := &models.Team{UserID: 1, Owner: &owner, ResourceScope: models.TeamScopeNorthbound, Name: "deleted-fixture", Status: models.TeamStatusDeleted, SharedMountPath: "/team", RedisEventsLastID: "0-0", CommunicationMode: "leader_mediated"}
	if err := repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	api := NewOwnerTeamAPI(sess, &memberRemovalPolicyEngine{}, ownerTeamTestInstances{repo: repository.NewInstanceRepository(sess)}, repository.NewUserRepository(sess), nil, true, 1)
	body, _ := json.Marshal(ownerTeamStoredRequest{Action: "teams.delete"})
	op := &models.NorthboundOperation{UserID: 1, Owner: &owner, TeamID: &team.ID, RequestPayload: string(body)}
	if _, err := api.execute(t.Context(), op); err != nil {
		t.Fatalf("completed native cleanup could not finish its original operation: %v", err)
	}
	if visible, err := api.store.Get(1, owner, team.ID); err != nil || visible != nil {
		t.Fatal("terminal replay exposed deleted Team through normal reads")
	}
	other := "other@example.com"
	op.Owner = &other
	if _, err := api.execute(t.Context(), op); err == nil {
		t.Fatal("cross-owner tombstone accepted")
	}
	op.Owner, op.UserID = &owner, 2
	if _, err := api.execute(t.Context(), op); err == nil {
		t.Fatal("cross-provider tombstone accepted")
	}
	op.UserID = 1
	body, _ = json.Marshal(ownerTeamStoredRequest{Action: "members.delete", MemberID: "worker"})
	op.RequestPayload = string(body)
	if _, err := api.execute(t.Context(), op); err == nil {
		t.Fatal("deleted Team authorized a different destructive action")
	}
}
