package northbound

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type memberRemovalPolicyEngine struct {
	services.TeamService
	deletes int
}

func (s *memberRemovalPolicyEngine) DeleteMember(int, int, string) error { s.deletes++; return nil }

func TestOwnerTeamMemberRemovalPolicyMySQL(t *testing.T) {
	session := ownerTeamMySQL(t)
	repo := repository.NewTeamRepository(session)
	instances := repository.NewInstanceRepository(session)
	owner := "member-policy@example.com"
	team := &models.Team{UserID: 1, Owner: &owner, ResourceScope: models.TeamScopeNorthbound, Name: "member-policy", Status: "running", SharedMountPath: "/team", RedisEventsLastID: "0-0", CommunicationMode: "leader_mediated"}
	if err := repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	member := &models.TeamMember{TeamID: team.ID, UserID: 1, MemberKey: "worker", DisplayName: "Worker", Role: "worker", RuntimeType: "opencode", InstanceMode: "lite", Status: "idle", Availability: "idle"}
	if err := repo.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	engine := &memberRemovalPolicyEngine{}
	api := NewOwnerTeamAPI(session, engine, ownerTeamTestInstances{repo: instances}, repository.NewUserRepository(session), nil, true, 1)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterOwnerTeamRoutes(router.Group("/owner"), func() *OwnerTeamAPI { return api }, func(*gin.Context) (*OwnerTeamIdentity, bool) {
		return &OwnerTeamIdentity{Owner: owner, SessionID: "member-policy", Source: "iei"}, true
	})
	for _, target := range []string{member.MemberKey, fmt.Sprint(member.ID)} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", fmt.Sprintf("/owner/teams/%d/members/%s", team.ID, target), strings.NewReader(`{"confirm_data_loss":true}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "member-removal-"+target)
		router.ServeHTTP(w, req)
		if w.Code != 409 || !strings.Contains(w.Body.String(), services.TeamMemberRemovalUnsupportedCode) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if n, err := session.Collection("northbound_operations").Find().Count(); err != nil || n != 0 {
		t.Fatalf("forbidden request queued operations: %d %v", n, err)
	}
	frozen, _ := json.Marshal(ownerTeamStoredRequest{Action: "members.delete", MemberID: member.MemberKey})
	op := &models.NorthboundOperation{UserID: 1, Owner: &owner, TeamID: &team.ID, RequestPayload: string(frozen)}
	if _, err := api.execute(context.Background(), op); err == nil || engine.deletes != 0 {
		t.Fatal("old queued removal started a new destructive action")
	}
	member.Status = models.TeamMemberStatusDeleting
	if err := repo.UpdateMember(member); err != nil {
		t.Fatal(err)
	}
	if _, err := api.execute(context.Background(), op); err != nil || engine.deletes != 1 {
		t.Fatalf("already-started cleanup blocked: %v, calls=%d", err, engine.deletes)
	}

	// Both Team types must reject deletion through ordinary instance routes;
	// the existing engine receives the matching Team ID for whole-Team cleanup.
	for _, scope := range []string{models.TeamScopeCM, models.TeamScopeNorthbound} {
		now := time.Now().UTC()
		i := &models.Instance{UserID: 1, Name: "guard-" + scope, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "running", OSType: "openclaw", OSVersion: "latest", StorageClass: "standard", MountPath: "/config", CreatedAt: now, UpdatedAt: now}
		if scope == models.TeamScopeNorthbound {
			i.Owner = &owner
		}
		if err := instances.Create(i); err != nil {
			t.Fatal(err)
		}
		tm := &models.Team{UserID: 1, ResourceScope: scope, Name: "guard-" + scope, Status: "running", SharedMountPath: "/team", RedisEventsLastID: "0-0", CommunicationMode: "leader_mediated"}
		if scope == models.TeamScopeNorthbound {
			tm.Owner = &owner
		}
		if err := repo.CreateTeam(tm); err != nil {
			t.Fatal(err)
		}
		m := &models.TeamMember{TeamID: tm.ID, UserID: 1, InstanceID: &i.ID, MemberKey: "worker", DisplayName: "Worker", Role: "worker", RuntimeType: "openclaw", InstanceMode: "lite", Status: "idle", Availability: "idle"}
		if err := repo.CreateMember(m); err != nil {
			t.Fatal(err)
		}
		if err := api.GuardInstanceMutation(i.ID, "delete", 0); err == nil {
			t.Fatalf("%s member deleted through ordinary route", scope)
		}
		if err := api.GuardInstanceMutation(i.ID, "delete", tm.ID); err != nil {
			t.Fatalf("%s whole-Team cleanup blocked: %v", scope, err)
		}
	}
}
