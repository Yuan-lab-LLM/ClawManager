package services

import (
	"clawreef/internal/models"
	"reflect"
	"testing"
)

func TestMemberDecorationDoesNotChangeRuntimeRoster(t *testing.T) {
	members := []CreateTeamMemberRequest{{MemberID: "leader", Name: "Chief", Role: "leader", IsLeader: true}, {MemberID: "worker", Name: "Research", Role: "worker", RoleProfile: &TeamMemberRoleProfileRequest{ProfileKey: "custom.team-template.9.worker", RoleHint: "data-analyst", Mission: "Analyze", CapabilityTags: []string{"data-analysis"}}}}
	first, err := planTeamMembers("Example", members)
	if err != nil {
		t.Fatal(err)
	}
	if first[1].IconKey != "data" {
		t.Fatal("automatic custom icon missing")
	}
	members[1].IconKey = "literature"
	second, err := planTeamMembers("Example", members)
	if err != nil {
		t.Fatal(err)
	}
	if second[1].IconKey != "literature" || !reflect.DeepEqual(first[1].Request.EnvironmentOverrides, second[1].Request.EnvironmentOverrides) {
		t.Fatal("icon changed compiled role environment")
	}
	team := &models.Team{ID: 150, Name: "Example", SharedMountPath: "/team", CommunicationMode: "leader_mediated"}
	a, err := buildTeamRosterConfig(team, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildTeamRosterConfig(team, second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("decoration changed Runtime roster/hash")
	}
	members[0].IconKey = "data"
	third, err := planTeamMembers("Example", members)
	if err != nil {
		t.Fatal(err)
	}
	if third[0].IconKey != "leader" {
		t.Fatal("decoration impersonated leader")
	}
}
