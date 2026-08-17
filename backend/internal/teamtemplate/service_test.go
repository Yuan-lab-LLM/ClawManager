package teamtemplate

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/aigateway"
	"clawreef/internal/models"
)

type fakeGateway struct {
	contents     []string
	calls        int
	noModels     bool
	statusCode   int
	responseBody []byte
	lastRequest  aigateway.ChatCompletionRequest
}

func (f *fakeGateway) ListAvailableModels() ([]aigateway.AvailableModel, error) {
	if f.noModels {
		return []aigateway.AvailableModel{}, nil
	}
	return []aigateway.AvailableModel{{ID: 0, DisplayName: "Auto", Provider: "gateway"}}, nil
}

func (f *fakeGateway) ChatCompletions(_ context.Context, _ int, req aigateway.ChatCompletionRequest) (*aigateway.ProxyResponse, string, error) {
	f.lastRequest = req
	if f.statusCode != 0 && (f.statusCode < 200 || f.statusCode >= 300) {
		f.calls++
		return &aigateway.ProxyResponse{StatusCode: f.statusCode, Body: f.responseBody}, "trace-test", nil
	}
	content := f.contents[f.calls]
	f.calls++
	body, _ := json.Marshal(map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message": map[string]interface{}{"role": "assistant", "content": content},
		}},
	})
	return &aigateway.ProxyResponse{StatusCode: 200, Body: body}, "trace-test", nil
}

type fakeRepository struct {
	item *models.CustomTeamTemplate
}

func (f *fakeRepository) Create(item *models.CustomTeamTemplate) error {
	copy := *item
	copy.ID = 9
	copy.CreatedAt = time.Now().UTC()
	copy.UpdatedAt = copy.CreatedAt
	f.item = &copy
	*item = copy
	return nil
}

func (f *fakeRepository) Update(item *models.CustomTeamTemplate) error {
	copy := *item
	copy.UpdatedAt = time.Now().UTC()
	f.item = &copy
	*item = copy
	return nil
}

func (f *fakeRepository) Delete(_, _ int) error { f.item = nil; return nil }

func (f *fakeRepository) GetByUserIDAndID(userID, id int) (*models.CustomTeamTemplate, error) {
	if f.item == nil || f.item.UserID != userID || f.item.ID != id {
		return nil, nil
	}
	copy := *f.item
	return &copy, nil
}

func (f *fakeRepository) ListByUserID(userID int) ([]models.CustomTeamTemplate, error) {
	if f.item == nil || f.item.UserID != userID {
		return []models.CustomTeamTemplate{}, nil
	}
	return []models.CustomTeamTemplate{*f.item}, nil
}

func TestGenerateAndAdjustWorkerPreserveTemplateInvariants(t *testing.T) {
	generated := `{
  "schemaVersion": 1,
  "name": "Research Team",
  "summary": "Research and synthesize evidence",
  "resolvedMemberCount": 3,
  "members": [
    {"memberId":"leader","displayName":"Leader","role":"leader","isLeader":true,"summary":"Coordinates","mission":"Coordinate","responsibilities":["Delegate"],"boundaries":[],"expectedInputs":[],"deliverables":["Final synthesis"],"acceptanceCriteria":["All work reconciled"],"collaborationNotes":[],"capabilityTags":[]},
    {"memberId":"researcher","displayName":"Researcher","role":"researcher","isLeader":false,"summary":"Finds evidence","mission":"Research","responsibilities":["Find sources"],"boundaries":["No final decision"],"expectedInputs":["Research question"],"deliverables":["Evidence notes"],"acceptanceCriteria":["Sources cited"],"collaborationNotes":["Report to Leader"],"capabilityTags":["research"]},
    {"memberId":"reviewer","displayName":"Reviewer","role":"reviewer","isLeader":false,"summary":"Checks evidence","mission":"Review","responsibilities":["Check claims"],"boundaries":[],"expectedInputs":["Evidence notes"],"deliverables":["Review verdict"],"acceptanceCriteria":["Claims traceable"],"collaborationNotes":["Report to Leader"],"capabilityTags":["review"]}
  ]
}`
	adjusted := `{"memberId":"researcher","displayName":"Primary Source Researcher","role":"literature-researcher","isLeader":true,"summary":"Finds primary evidence","mission":"Research primary sources","responsibilities":["Find primary sources"],"boundaries":["Do not review final output"],"expectedInputs":["Research question"],"deliverables":["Cited evidence notes"],"acceptanceCriteria":["Every claim has a source"],"collaborationNotes":["Report to Leader"],"capabilityTags":["research"]}`
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{generated, adjusted}}
	service := NewService(repo, gateway)
	count := 3
	payload, err := service.Generate(context.Background(), 4, GenerateRequest{Intent: "研究一个市场", MemberCount: &count})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if payload.Spec.Members[0].MemberID != "leader" || !payload.Spec.Members[0].IsLeader {
		t.Fatalf("leader invariant not preserved: %#v", payload.Spec.Members[0])
	}
	if len(gateway.lastRequest.Messages) != 2 || gateway.lastRequest.Messages[0].Role != "system" || gateway.lastRequest.Messages[1].Role != "user" {
		t.Fatalf("gateway request messages were not preserved: %#v", gateway.lastRequest.Messages)
	}

	updated, err := service.AdjustMember(context.Background(), 4, payload.ID, "researcher", AdjustMemberRequest{
		Instruction: "只使用一手来源", ExpectedRevision: payload.Revision,
	})
	if err != nil {
		t.Fatalf("AdjustMember returned error: %v", err)
	}
	worker := updated.Spec.Members[1]
	if worker.MemberID != "researcher" || worker.IsLeader {
		t.Fatalf("worker identity invariant not preserved: %#v", worker)
	}
	if worker.Role != "literature-researcher" || updated.Revision != 2 {
		t.Fatalf("adjusted worker/revision = %#v / %d", worker, updated.Revision)
	}
}

func TestAdjustLeaderOnlyChangesDomainOverlayAndPreservesRoster(t *testing.T) {
	generated := `{
  "schemaVersion": 1,
  "name": "Briefing Team",
  "summary": "Research and publish a daily briefing",
  "resolvedMemberCount": 3,
  "members": [
    {"memberId":"leader","displayName":"Chief Editor","role":"leader","isLeader":true,"summary":"Coordinates the briefing","mission":"Coordinate research and publication","responsibilities":["Delegate research","Reconcile findings"],"boundaries":["Do not replace specialist research"],"expectedInputs":["Research results"],"deliverables":["Final briefing"],"acceptanceCriteria":["All findings reconciled"],"collaborationNotes":["Coordinate researcher and reviewer"],"capabilityTags":["orchestration"]},
    {"memberId":"researcher","displayName":"Researcher","role":"researcher","isLeader":false,"summary":"Finds primary sources","mission":"Research","responsibilities":["Find sources"],"boundaries":[],"expectedInputs":["Research question"],"deliverables":["Evidence notes"],"acceptanceCriteria":["Sources cited"],"collaborationNotes":["Report to Leader"],"capabilityTags":["research"]},
    {"memberId":"reviewer","displayName":"Reviewer","role":"reviewer","isLeader":false,"summary":"Reviews claims","mission":"Review","responsibilities":["Check evidence"],"boundaries":[],"expectedInputs":["Evidence notes"],"deliverables":["Review verdict"],"acceptanceCriteria":["Claims traceable"],"collaborationNotes":["Report to Leader"],"capabilityTags":["review"]}
  ]
}`
	// Deliberately violate every immutable Leader identity field. The service
	// must treat this as a domain overlay only and restore the canonical Leader.
	adjusted := `{"memberId":"independent-writer","displayName":"Independent Writer","role":"writer","isLeader":false,"summary":"Focuses on executive readers","mission":"Shape the final briefing for executives","responsibilities":["Prioritize executive insights"],"boundaries":["Do not perform specialist research"],"expectedInputs":["Research and review results"],"deliverables":["Executive briefing"],"acceptanceCriteria":["Recommendations are actionable"],"collaborationNotes":["Use researcher and reviewer outputs"],"capabilityTags":["editorial"]}`
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{generated, adjusted}}
	service := NewService(repo, gateway)
	count := 3
	created, err := service.Generate(context.Background(), 5, GenerateRequest{
		Intent: "制作每日行业简报", MemberCount: &count,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	workersBefore := append([]MemberSpec(nil), created.Spec.Members[1:]...)

	updated, err := service.AdjustMember(context.Background(), 5, created.ID, "leader", AdjustMemberRequest{
		Instruction: "更关注高管决策信息", ExpectedRevision: created.Revision,
	})
	if err != nil {
		t.Fatalf("AdjustMember Leader returned error: %v", err)
	}
	leader := updated.Spec.Members[0]
	if leader.MemberID != "leader" || leader.Role != "leader" || !leader.IsLeader {
		t.Fatalf("Leader identity invariant not preserved: %#v", leader)
	}
	if leader.DisplayName != "Chief Editor" {
		t.Fatalf("Leader display name = %q, want the existing identity", leader.DisplayName)
	}
	if leader.Mission != "Shape the final briefing for executives" || updated.Revision != 2 {
		t.Fatalf("Leader domain overlay/revision = %#v / %d", leader, updated.Revision)
	}
	if got, want := updated.Spec.Members[1:], workersBefore; !reflect.DeepEqual(got, want) {
		t.Fatalf("Leader adjustment changed Worker roster:\n got %#v\nwant %#v", got, want)
	}
	requestText := flattenContent(gateway.lastRequest.Messages[0].Content) + "\n" + flattenContent(gateway.lastRequest.Messages[1].Content)
	for _, expected := range []string{
		"固定 Leader 主模板会由系统在创建 Team 时独立继承",
		`"memberId":"researcher"`,
		`"memberId":"reviewer"`,
		"只调整 Leader 的领域延展职责",
	} {
		if !strings.Contains(requestText, expected) {
			t.Fatalf("Leader adjustment prompt missing %q:\n%s", expected, requestText)
		}
	}
}

func TestRegenerateLeaderRemainsUnsupported(t *testing.T) {
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{generatedTeamJSON(t, "Team", 2)}}
	service := NewService(repo, gateway)
	count := 2
	created, err := service.Generate(context.Background(), 6, GenerateRequest{Intent: "test", MemberCount: &count})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	_, err = service.RegenerateMember(context.Background(), 6, created.ID, "leader", RegenerateMemberRequest{
		ExpectedRevision: created.Revision,
	})
	if err == nil || !strings.Contains(err.Error(), "only supports responsibility adjustment") {
		t.Fatalf("RegenerateMember Leader error = %v", err)
	}
	if gateway.calls != 1 {
		t.Fatalf("Leader regenerate made %d model calls, want only initial generation", gateway.calls)
	}
}

func TestGenerateRejectsRequestedCountMismatch(t *testing.T) {
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{`{"schemaVersion":1,"name":"Bad","summary":"","resolvedMemberCount":2,"members":[{"memberId":"leader","displayName":"Leader","role":"leader","isLeader":true},{"memberId":"worker","displayName":"Worker","role":"worker","isLeader":false}]}`}}
	service := NewService(repo, gateway)
	count := 4
	if _, err := service.Generate(context.Background(), 1, GenerateRequest{Intent: "test", MemberCount: &count}); err == nil {
		t.Fatal("expected member-count mismatch error")
	}
}

func TestGenerateReturnsFriendlyMissingModelErrorBeforeCallingGateway(t *testing.T) {
	repo := &fakeRepository{}
	gateway := &fakeGateway{noModels: true}
	service := NewService(repo, gateway)
	_, err := service.Generate(context.Background(), 1, GenerateRequest{Intent: "研究一个市场"})
	if err == nil || err.Error() != "custom team template requires an active AI model" {
		t.Fatalf("Generate error = %v, want missing-model guidance error", err)
	}
	if gateway.calls != 0 {
		t.Fatalf("ChatCompletions calls = %d, want 0", gateway.calls)
	}
}

func TestGenerateReturnsProviderFailureInsteadOfNoContent(t *testing.T) {
	repo := &fakeRepository{}
	gateway := &fakeGateway{
		statusCode:   400,
		responseBody: []byte(`{"error":{"message":"field messages is required","type":"one_api_error"}}`),
	}
	service := NewService(repo, gateway)
	_, err := service.Generate(context.Background(), 1, GenerateRequest{Intent: "研究一个市场"})
	if err == nil || err.Error() != "custom team template model request failed: field messages is required" {
		t.Fatalf("Generate error = %v, want upstream provider error", err)
	}
}

func TestGenerateNormalizesNarrowModelJSONTypeVariations(t *testing.T) {
	generated := `{
  "schemaVersion": "1.0",
  "name": "App开发团队",
  "summary": "交付应用",
  "resolvedMemberCount": "2",
  "members": [
    {"memberId":"leader","displayName":"负责人","role":"leader","isLeader":"true","summary":"协调","mission":"协调交付","responsibilities":"拆解任务","boundaries":"不替代 Worker 实施","expectedInputs":"业务目标","deliverables":"最终结果","acceptanceCriteria":"完成验收","collaborationNotes":"统一协调","capabilityTags":"项目管理"},
    {"memberId":"developer","displayName":"开发工程师","role":"worker","isLeader":"false","summary":"开发","mission":"实现应用","responsibilities":["编写代码"],"boundaries":"不负责最终决策","expectedInputs":"设计稿","deliverables":"应用代码","acceptanceCriteria":"功能可用","collaborationNotes":"向 Leader 汇报","capabilityTags":["开发"]}
  ]
}`
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{generated}}
	service := NewService(repo, gateway)
	count := 2
	payload, err := service.Generate(context.Background(), 1, GenerateRequest{Intent: "开发一个 App", MemberCount: &count})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if payload.Spec.SchemaVersion != 1 || payload.Spec.ResolvedMemberCount != 2 {
		t.Fatalf("normalized template numbers = version %d, count %d", payload.Spec.SchemaVersion, payload.Spec.ResolvedMemberCount)
	}
	if !payload.Spec.Members[0].IsLeader || payload.Spec.Members[1].IsLeader {
		t.Fatalf("normalized leader flags are incorrect: %#v", payload.Spec.Members)
	}
	if got := payload.Spec.Members[1].Deliverables; len(got) != 1 || got[0] != "应用代码" {
		t.Fatalf("normalized scalar list = %#v", got)
	}
	var persisted TemplateSpec
	if err := json.Unmarshal([]byte(repo.item.SpecJSON), &persisted); err != nil {
		t.Fatalf("persisted canonical spec cannot be decoded: %v", err)
	}
	if !strings.Contains(repo.item.SpecJSON, `"schemaVersion":1`) || !strings.Contains(repo.item.SpecJSON, `"deliverables":["应用代码"]`) {
		t.Fatalf("persisted spec is not canonical: %s", repo.item.SpecJSON)
	}
}

func TestReviseUpdatesWholeTeamAndPreservesUserName(t *testing.T) {
	repo := &fakeRepository{}
	gateway := &fakeGateway{contents: []string{
		generatedTeamJSON(t, "Generated First Name", 2),
		generatedTeamJSON(t, "Generated Revised Name", 4),
		generatedTeamJSON(t, "Generated Again Name", 4),
	}}
	service := NewService(repo, gateway)
	initialCount := 2
	created, err := service.Generate(context.Background(), 7, GenerateRequest{
		Intent: "build an app", MemberCount: &initialCount,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	renamed, err := service.UpdateMetadata(7, created.ID, UpdateMetadataRequest{
		Name: "My Product Team", ExpectedRevision: created.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateMetadata returned error: %v", err)
	}

	revisedCount := 4
	revised, err := service.Revise(context.Background(), 7, created.ID, ReviseRequest{
		Name:             "My Product Team",
		Intent:           "build and validate a mobile app",
		MemberCount:      &revisedCount,
		ExpectedRevision: renamed.Revision,
	})
	if err != nil {
		t.Fatalf("Revise returned error: %v", err)
	}
	if revised.Name != "My Product Team" || revised.Spec.Name != "My Product Team" {
		t.Fatalf("revised names = %q / %q, want user name", revised.Name, revised.Spec.Name)
	}
	if revised.Intent != "build and validate a mobile app" || revised.RequestedMemberCount == nil || *revised.RequestedMemberCount != 4 {
		t.Fatalf("revised intent/count = %q / %#v", revised.Intent, revised.RequestedMemberCount)
	}
	if revised.ResolvedMemberCount != 4 || len(revised.Spec.Members) != 4 || !revised.Spec.Members[0].IsLeader {
		t.Fatalf("revised team invariants not preserved: %#v", revised.Spec)
	}

	regenerated, err := service.Regenerate(context.Background(), 7, created.ID, RegenerateRequest{
		ExpectedRevision: revised.Revision,
	})
	if err != nil {
		t.Fatalf("Regenerate returned error: %v", err)
	}
	if regenerated.Name != "My Product Team" || regenerated.Spec.Name != "My Product Team" {
		t.Fatalf("regenerated names = %q / %q, want user name", regenerated.Name, regenerated.Spec.Name)
	}
}

func generatedTeamJSON(t *testing.T, name string, count int) string {
	t.Helper()
	members := make([]map[string]interface{}, 0, count)
	for index := 0; index < count; index++ {
		memberID := string(rune('a' + index))
		role := "worker"
		isLeader := false
		if index == 0 {
			memberID = "leader"
			role = "leader"
			isLeader = true
		}
		members = append(members, map[string]interface{}{
			"memberId": memberID, "displayName": memberID, "role": role,
			"isLeader": isLeader, "summary": "summary", "mission": "mission",
			"responsibilities": []string{"responsibility"}, "boundaries": []string{},
			"expectedInputs": []string{}, "deliverables": []string{"deliverable"},
			"acceptanceCriteria": []string{"accepted"}, "collaborationNotes": []string{},
			"capabilityTags": []string{},
		})
	}
	raw, err := json.Marshal(map[string]interface{}{
		"schemaVersion": 1, "name": name, "summary": "summary",
		"resolvedMemberCount": count, "members": members,
	})
	if err != nil {
		t.Fatalf("failed to encode generated team fixture: %v", err)
	}
	return string(raw)
}
