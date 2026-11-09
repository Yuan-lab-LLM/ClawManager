package teamtemplate

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCustomIconPersistenceAndRoleIsolation(t *testing.T) {
	spec := TemplateSpec{SchemaVersion: 1, Name: "Custom", ResolvedMemberCount: 2, Members: []MemberSpec{
		{MemberID: "leader", DisplayName: "主编", Role: "leader", IsLeader: true, Mission: "Coordinate"},
		{MemberID: "analyst", DisplayName: "趋势专家", Role: "数据分析师", Mission: "Analyze", Responsibilities: []string{"Analyze evidence"}},
	}}
	if err := validateAndNormalizeSpec(&spec, nil); err != nil {
		t.Fatal(err)
	}
	if spec.Members[1].IconKey != "data" {
		t.Fatalf("Chinese hint was lost: %#v", spec.Members[1])
	}
	raw, _ := json.Marshal(spec)
	repo := &fakeRepository{item: &models.CustomTeamTemplate{ID: 9, UserID: 4, Name: "Custom", SpecJSON: string(raw), Revision: 1}}
	gateway := &fakeGateway{}
	service := NewService(repo, gateway)
	before := StableRoleProfilePrompt(spec.Members[1])
	selected, err := service.UpdateMemberIcon(4, 9, "analyst", UpdateMemberIconRequest{IconOverride: "editor", ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Spec.Members[1].IconKey != "editor" || gateway.calls != 0 || before != StableRoleProfilePrompt(selected.Spec.Members[1]) {
		t.Fatal("decoration altered role or called model")
	}
	loaded, err := NewService(repo, gateway).Get(4, 9)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Spec.Members[1].IconKey != "editor" || loaded.Spec.Members[1].AutoIconKey != "data" {
		t.Fatal("choice lost on JSON/reload")
	}
	if _, err := service.UpdateMemberIcon(4, 9, "analyst", UpdateMemberIconRequest{IconOverride: "qa", ExpectedRevision: 1}); err == nil {
		t.Fatal("stale edit accepted")
	}
	if _, err := service.UpdateMemberIcon(5, 9, "analyst", UpdateMemberIconRequest{IconOverride: "qa", ExpectedRevision: 2}); err == nil {
		t.Fatal("another owner accepted")
	}
	if _, err := service.UpdateMemberIcon(4, 9, "leader", UpdateMemberIconRequest{IconOverride: "qa", ExpectedRevision: 2}); err == nil {
		t.Fatal("leader became worker badge")
	}
	if _, err := service.UpdateMemberIcon(4, 9, "analyst", UpdateMemberIconRequest{IconOverride: "https://bad.test/icon.svg", ExpectedRevision: 2}); err == nil {
		t.Fatal("arbitrary asset accepted")
	}
	cleared, err := service.UpdateMemberIcon(4, 9, "analyst", UpdateMemberIconRequest{ExpectedRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Spec.Members[1].IconKey != "data" {
		t.Fatal("automatic Chinese semantic snapshot not restored")
	}
	if !reflect.DeepEqual(cleared.Spec.Members[1].Responsibilities, spec.Members[1].Responsibilities) {
		t.Fatal("responsibilities modified")
	}

	// Natural-language adjustment must preserve explicit choice while updating the
	// auto snapshot; model-supplied decoration is not authoritative.
	_, err = service.UpdateMemberIcon(4, 9, "analyst", UpdateMemberIconRequest{IconOverride: "editor", ExpectedRevision: 3})
	if err != nil {
		t.Fatal(err)
	}
	gateway.contents = []string{`{"memberId":"bad","displayName":"数据工程师","role":"backend-developer","isLeader":false,"mission":"Integrate APIs","iconOverride":"security","autoIconKey":"security","capabilityTags":["backend"]}`}
	adjusted, err := service.AdjustMember(context.Background(), 4, 9, "analyst", AdjustMemberRequest{Instruction: "Integrate APIs", ExpectedRevision: 4})
	if err != nil {
		t.Fatal(err)
	}
	if adjusted.Spec.Members[1].IconKey != "editor" || adjusted.Spec.Members[1].AutoIconKey != "backend" || adjusted.Spec.Members[1].MemberID != "analyst" {
		t.Fatalf("adjustment identity/decoration: %#v", adjusted.Spec.Members[1])
	}
}

func TestLegacyTemplateIconsDoNotRequireMigrationOrModel(t *testing.T) {
	raw := `{"schemaVersion":1,"name":"Legacy","members":[{"memberId":"leader","role":"leader","isLeader":true},{"memberId":"news","role":"worker","displayName":"资讯采集员","capabilityTags":["news-collection"]}]}`
	repo := &fakeRepository{item: &models.CustomTeamTemplate{ID: 9, UserID: 4, SpecJSON: raw, Revision: 1}}
	service := NewService(repo, &fakeGateway{noModels: true})
	got, err := service.Get(4, 9)
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.Members[0].IconKey != "leader" || got.Spec.Members[1].IconKey != "news" || repo.item.SpecJSON != raw {
		t.Fatal("legacy read changed source or failed fallback")
	}
}
