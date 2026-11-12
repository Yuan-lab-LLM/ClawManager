package teamtemplate

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCustomTemplateCreationIdentity(t *testing.T) {
	for _, role := range []string{"leader", "team-leader", "team_leader", "Team Leader"} {
		s := TemplateSpec{Members: []MemberSpec{{MemberID: "leader", IsLeader: true, Role: "leader"}, {MemberID: "worker", Role: role}}}
		if err := validateAndNormalizeSpec(&s, nil); err == nil {
			t.Fatalf("ambiguous Worker role accepted: %s", role)
		}
	}
}

func TestAdjustWorkerCannotBecomeLeaderByRole(t *testing.T) {
	s := TemplateSpec{Members: []MemberSpec{{MemberID: "leader", IsLeader: true, Role: "leader"}, {MemberID: "worker", Role: "developer", Mission: "Implement"}}}
	if err := validateAndNormalizeSpec(&s, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s)
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{string(raw), `{"memberId":"leader","role":"team-leader","isLeader":true,"mission":"New implementation duties"}`}}
	service := NewService(repo, gateway)
	created, err := service.Generate(context.Background(), 1, GenerateRequest{Intent: "Build"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.AdjustMember(context.Background(), 1, created.ID, "worker", AdjustMemberRequest{Instruction: "Improve implementation", ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	w := updated.Spec.Members[1]
	if w.MemberID != "worker" || w.IsLeader || w.Role != "developer" {
		t.Fatalf("identity escaped: %+v", w)
	}
}
