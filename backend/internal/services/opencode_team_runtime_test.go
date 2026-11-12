package services

import (
	"clawreef/internal/models"
	"testing"
)

func TestOpenCodeTeamRequiresCompatibleServingRuntime(t *testing.T) {
	env := `{"CLAWMANAGER_TEAM_ENABLED":"true","CLAWMANAGER_TEAM_ID":"4"}`
	pods := []models.RuntimePod{{ID: 1}, {ID: 2, TeamPluginVersion: optionalString(openCodeTeamProtocol)}}
	instance := &models.Instance{Type: "opencode", EnvironmentOverridesJSON: &env}
	actual := filterOpenCodeTeamPods(instance, pods)
	if len(actual) != 1 || actual[0].ID != 2 {
		t.Fatalf("unsupported Team runtime accepted: %v", actual)
	}
	instance.EnvironmentOverridesJSON = nil
	if len(filterOpenCodeTeamPods(instance, pods)) != 2 {
		t.Fatal("ordinary OpenCode scheduling regressed")
	}
}

func TestPlanOpenCodeTeamRolesAndModes(t *testing.T) {
	for _, mode := range []string{"lite", "pro"} {
		plans, err := planTeamMembers("coding", []CreateTeamMemberRequest{
			{MemberID: "lead", RuntimeType: "opencode", IsLeader: true, InstanceMode: mode},
			{MemberID: "worker", RuntimeType: "opencode", Role: "worker", InstanceMode: mode},
		})
		if err != nil || len(plans) != 2 || plans[0].RuntimeType != "opencode" || !plans[0].IsLeader {
			t.Fatalf("%s: plans=%v error=%v", mode, plans, err)
		}
	}
	if _, err := normalizeTeamMemberRuntimeType("unknown"); err == nil {
		t.Fatal("unknown runtime accepted")
	}
}
