package services

import (
	"clawreef/internal/models"
	"strings"
	"testing"
	"time"
)

func TestDshTeamAdmissionAndServingCapability(t *testing.T) {
	now := time.Now()
	env := `{"CLAWMANAGER_TEAM_ENABLED":"true"}`
	instance := &models.Instance{Type: RuntimeTypeDeepSeekHarness, EnvironmentOverridesJSON: &env}
	pods := []models.RuntimePod{{ID: 1, State: "ready", Capacity: 4, LastSeenAt: &now}, {ID: 2, State: "ready", Capacity: 2, UsedSlots: 1, LastSeenAt: &now, TeamPluginVersion: optionalString("dsh-team-v1")}}
	if got := filterOpenCodeTeamPods(instance, pods); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("DSH capability filter: %v", got)
	}
	instance.EnvironmentOverridesJSON = nil
	if len(filterOpenCodeTeamPods(instance, pods)) != 2 {
		t.Fatal("ordinary DSH was restricted")
	}
	s := &instanceService{runtimePodRepo: &fakeRuntimePodRepo{schedulable: pods}}
	req := CreateInstanceRequest{Type: RuntimeTypeDeepSeekHarness, Team: &TeamInstanceConfig{}}
	if err := s.ValidateTeamRuntimeRequests([]CreateInstanceRequest{req}); err != nil {
		t.Fatal(err)
	}
	if s.ValidateTeamRuntimeRequests([]CreateInstanceRequest{req, req}) == nil {
		t.Fatal("insufficient compatible capacity accepted")
	}
	s.runtimePodRepo = nil
	if s.ValidateTeamRuntimeRequests([]CreateInstanceRequest{req}) == nil {
		t.Fatal("missing runtime proof accepted")
	}
	req.Team = nil
	if err := s.ValidateTeamRuntimeRequests([]CreateInstanceRequest{req}); err != nil {
		t.Fatal("ordinary Pro restricted", err)
	}
	// No repository or resource service is installed: a valid new DSH Team must
	// fail closed before even querying/deleting a same-name failed Team.
	request := CreateTeamRequest{Name: "dsh", Members: []CreateTeamMemberRequest{{MemberID: "leader", IsLeader: true, RuntimeType: "openclaw"}, {MemberID: "worker", Role: "worker", RuntimeType: RuntimeTypeDeepSeekHarness}}}
	if _, err := (&teamService{instanceService: s}).CreateTeam(1, request); err == nil {
		t.Fatal("no compatible pool accepted")
	}
}

func TestDshTeamLeaderAndProRejected(t *testing.T) {
	prompt := buildTeamMemberAgentsMarkdown(&models.Team{ID: 1}, plannedTeamMember{RuntimeType: RuntimeTypeDeepSeekHarness})
	if !strings.Contains(prompt, "DeepSeek Harness") || !strings.Contains(prompt, "If no browser tool is installed") {
		t.Fatal("DSH prompt misstates native capabilities")
	}
	for _, leader := range []bool{false, true} {
		req := CreateTeamRequest{Members: []CreateTeamMemberRequest{{MemberID: "leader", IsLeader: true, RuntimeType: "openclaw"}, {MemberID: "worker", Role: "worker", RuntimeType: RuntimeTypeDeepSeekHarness}}}
		if leader {
			req.Members[0].RuntimeType = RuntimeTypeDeepSeekHarness
		} else {
			req.Members[1].InstanceMode = InstanceModePro
		}
		if ValidateNewTeamRequest(req) == nil {
			t.Fatal("unsupported DSH Team role/mode accepted")
		}
	}
}

func TestDshProtectedPreviewRetainsImageAndGenerationGuard(t *testing.T) {
	for _, runtime := range []string{RuntimeTypeDeepSeekHarness, RuntimeTypeOpenCode} {
		p, teams, id, _ := previewFixture(t)
		teams.member.RuntimeType = runtime
		p.redis.instances.(managedInstanceFixture).instance.Type = runtime
		if _, err := p.Sign(t.Context(), id, "site/index.html", "interactive"); err != nil {
			t.Fatal(runtime, err)
		}
	}
}
