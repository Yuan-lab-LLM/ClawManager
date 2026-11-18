package services

import "testing"

func TestNewTeamCreationPolicy(t *testing.T) {
	for _, runtime := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		base := CreateTeamRequest{Members: []CreateTeamMemberRequest{{MemberID: "leader", IsLeader: true, RuntimeType: "openclaw"}, {MemberID: "worker", Role: "worker", RuntimeType: runtime}}}
		if err := ValidateNewTeamRequest(base); err != nil {
			t.Fatalf("Lite %s: %v", runtime, err)
		}
		for _, field := range []string{"leader-runtime", "leader-mode", "worker-mode", "conflicting-mode", "worker-role", "collaboration"} {
			t.Run(runtime+"/"+field, func(t *testing.T) {
				r := base
				r.Members = append([]CreateTeamMemberRequest(nil), base.Members...)
				switch field {
				case "leader-runtime":
					r.Members[0].RuntimeType = "opencode"
				case "leader-mode":
					r.Members[0].InstanceMode = "pro"
				case "worker-mode":
					r.Members[1].Mode = "PRO"
				case "conflicting-mode":
					r.Members[1].Mode = "lite"
					r.Members[1].InstanceMode = "pro"
				case "worker-role":
					r.Members[1].Role = "team_leader"
				case "collaboration":
					r.CommunicationMode = "full_mesh"
				}
				if ValidateNewTeamRequest(r) == nil {
					t.Fatal("unsafe new Team accepted")
				}
				// Invalid CM requests must fail before any repository/resource call.
				if _, err := (&teamService{}).CreateTeam(1, r); err == nil {
					t.Fatal("creation bypass")
				}
			})
		}
	}
	// Historical planners are also used for already-created resources. Keep
	// their representation support separate from new creation admission.
	if _, err := planTeamMembers("old", []CreateTeamMemberRequest{{MemberID: "lead", IsLeader: true, RuntimeType: "opencode", InstanceMode: "pro"}}); err != nil {
		t.Fatal(err)
	}
	if mode, ok := NormalizeInstanceMode("pro"); !ok || mode != InstanceModePro {
		t.Fatal("ordinary Pro was disabled")
	}
}
