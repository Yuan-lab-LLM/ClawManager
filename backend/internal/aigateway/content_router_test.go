package aigateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

func routingRequest(text string) ChatCompletionRequest {
	return ChatCompletionRequest{Model: "auto", Messages: []ChatMessage{{Role: "user", Content: text}}}
}

func routingModels() []models.LLMModel {
	return []models.LLMModel{
		{ID: 1, DisplayName: "cheap", ModelGroup: "basic", ProviderType: models.ProviderTypeOpenAICompatible, IsActive: true, IsSecure: true, QualityTier: 1, InputPrice: .5, OutputPrice: .5, Currency: "USD", ContextWindowTokens: 100000},
		{ID: 2, DisplayName: "capable", ModelGroup: "advanced", ProviderType: models.ProviderTypeOpenAICompatible, IsActive: true, IsSecure: true, QualityTier: 4, InputPrice: 1, OutputPrice: 1, Currency: "USD", ContextWindowTokens: 100000},
	}
}

func TestClassifyRoutingDifficulty(t *testing.T) {
	for _, tc := range []struct {
		text string
		tier int
	}{
		{"你好", 1}, {"1+1 等于几", 1}, {"What is the capital of France?", 1},
		{"写一个 Python 函数排序", 2}, {"Implement a sorting function", 2},
		{"设计分布式系统架构并分析权衡", 3}, {"Design a distributed architecture", 3},
		{"证明这个定理", 4}, {"Prove the theorem by mathematical induction", 4},
	} {
		t.Run(tc.text, func(t *testing.T) {
			got := classifyRoutingDifficulty(routingRequest(tc.text).Messages)
			if got.RequiredTier != tc.tier {
				t.Fatalf("classification = %+v; want tier %d", got, tc.tier)
			}
		})
	}
}

func TestRoutingIgnoresSystemAndToolScaffolding(t *testing.T) {
	messages := []ChatMessage{
		{Role: "system", Content: strings.Repeat("prove architecture code ", 1000)},
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "architecture proof"},
		{Role: "tool", Content: "complex theorem"},
	}
	if d := classifyRoutingDifficulty(messages); d.RequiredTier != 1 {
		t.Fatalf("scaffolding affected classification: %+v", d)
	}
	messages[1].Content = "证明这个定理"
	messages = append(messages, ChatMessage{Role: "user", Content: "继续"})
	if d := classifyRoutingDifficulty(messages); d.RequiredTier != 4 {
		t.Fatalf("continuation lost original task: %+v", d)
	}
	messages = append(messages, ChatMessage{Role: "user", Content: "你好"})
	if d := classifyRoutingDifficulty(messages); d.RequiredTier != 1 {
		t.Fatalf("new question inherited old difficulty: %+v", d)
	}
}

func TestContentRoutingCostAndCapability(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{{"你好", 1}, {"设计分布式架构", 2}, {"证明定理", 2}} {
		gateway := &service{modelRepo: &stubLLMModelRepository{active: routingModels()}}
		got, d, err := gateway.selectContentModel(t.Context(), routingRequest(tc.text), false)
		if err != nil || got.ID != tc.want || d.ModelID != tc.want {
			t.Fatalf("routing %q = %+v %+v %v", tc.text, got, d, err)
		}
	}
}

func TestContentRoutingConstraints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]models.LLMModel, *ChatCompletionRequest)
		secure bool
		want   int
	}{
		{"allowlist", func(_ []models.LLMModel, r *ChatCompletionRequest) { r.AllowedModelGroups = []string{"advanced"} }, false, 2},
		{"secure", func(m []models.LLMModel, _ *ChatCompletionRequest) { m[0].IsSecure = false }, true, 2},
		{"inactive", func(m []models.LLMModel, _ *ChatCompletionRequest) { m[0].IsActive = false }, false, 2},
		{"context", func(m []models.LLMModel, r *ChatCompletionRequest) {
			m[0].ContextWindowTokens = 50
			r.Messages = append([]ChatMessage{{Role: "system", Content: strings.Repeat("a", 500)}}, r.Messages...)
		}, false, 2},
		{"no_allowed", func(_ []models.LLMModel, r *ChatCompletionRequest) { r.AllowedModelGroups = []string{"missing"} }, false, 0},
		{"no_secure", func(m []models.LLMModel, _ *ChatCompletionRequest) { m[0].IsSecure = false; m[1].IsSecure = false }, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, r := routingModels(), routingRequest("你好")
			tc.change(m, &r)
			gateway := &service{modelRepo: &stubLLMModelRepository{active: m}}
			got, _, err := gateway.selectContentModel(t.Context(), r, tc.secure)
			if tc.want == 0 {
				if err == nil {
					t.Fatal("expected fail closed")
				}
				return
			}
			if err != nil || got.ID != tc.want {
				t.Fatalf("selected %+v, error %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestContentRoutingContextIncludesReservedOutput(t *testing.T) {
	m := routingModels()
	r := routingRequest("你好")
	prompt, output := routingTokenEstimate(r)
	m[0].ContextWindowTokens = prompt + output - 1
	m[1].ContextWindowTokens = prompt + output
	gateway := &service{modelRepo: &stubLLMModelRepository{active: m}}
	got, decision, err := gateway.selectContentModel(t.Context(), r, false)
	if err != nil || got.ID != 2 {
		t.Fatalf("selected %+v, decision %+v, error %v; want exact-fit model 2", got, decision, err)
	}
	if len(decision.Rejections) == 0 || !slices.Contains(decision.Rejections[0].Reasons, "context_capacity_exceeded") {
		t.Fatalf("capacity rejection missing: %+v", decision.Rejections)
	}
}

func TestContentRoutingLegacyAndMixedCurrencyWarnings(t *testing.T) {
	m := routingModels()
	for i := range m {
		m[i].QualityTier = 0
	}
	gateway := &service{modelRepo: &stubLLMModelRepository{active: m}}
	got, d, err := gateway.selectContentModel(t.Context(), routingRequest("证明定理"), false)
	if err != nil || got.ID != 1 || !slices.Contains(d.Warnings, "insufficient_quality_tier_best_available") || !slices.Contains(d.Warnings, "unclassified_model_assumed_basic") {
		t.Fatalf("legacy routing = %+v %+v %v", got, d, err)
	}
	m[0].Currency = "CNY"
	m[0].InputPrice, m[0].OutputPrice = 100, 100
	_, d, err = gateway.selectContentModel(t.Context(), routingRequest("你好"), false)
	if err != nil || !slices.Contains(d.Warnings, "mixed_currencies_priority_order_no_cost_comparison") {
		t.Fatalf("mixed currency warning missing: %+v %v", d, err)
	}
}

func TestContentRoutingPreservesGroupPriority(t *testing.T) {
	m := routingModels()
	m[0].ModelGroup, m[1].ModelGroup = "same", "same"
	m[0].DeploymentPriority, m[1].DeploymentPriority = 200, 10
	gateway := &service{modelRepo: &stubLLMModelRepository{active: m}}
	got, _, err := gateway.selectContentModel(t.Context(), routingRequest("你好"), false)
	if err != nil || got.ID != 2 {
		t.Fatalf("group primary not preserved: %+v %v", got, err)
	}
}

func TestRoutingCostUsesInputAndOutputPrices(t *testing.T) {
	m := routingModels()
	m[0].InputPrice, m[0].OutputPrice = 0.1, 10
	m[1].InputPrice, m[1].OutputPrice = 10, 0.1
	gateway := &service{modelRepo: &stubLLMModelRepository{active: m}}
	r := routingRequest("你好")
	got, _, err := gateway.selectContentModel(t.Context(), r, false)
	if err != nil || got.ID != 2 {
		t.Fatalf("output cost was ignored: %+v %v", got, err)
	}
	limit := 1
	r.MaxTokens = &limit
	r.Messages = append([]ChatMessage{{Role: "system", Content: strings.Repeat("context ", 2000)}}, r.Messages...)
	got, _, err = gateway.selectContentModel(t.Context(), r, false)
	if err != nil || got.ID != 1 {
		t.Fatalf("input cost was ignored: %+v %v", got, err)
	}
}

func TestRoutingCapacityIncludesChineseHistoryAndTools(t *testing.T) {
	r := routingRequest("你好")
	base, _ := routingTokenEstimate(r)
	r.Messages = append([]ChatMessage{{Role: "system", Content: strings.Repeat("中", 1000)}}, r.Messages...)
	withHistory, _ := routingTokenEstimate(r)
	if withHistory < base+1000 {
		t.Fatal("Chinese context substantially underestimated")
	}
	r.Tools = json.RawMessage(`[{"type":"function","function":{"name":"f","description":"` + strings.Repeat("tool ", 1000) + `"}}]`)
	withTools, _ := routingTokenEstimate(r)
	if withTools <= withHistory {
		t.Fatal("tool schema omitted from estimate")
	}
	if d := classifyRoutingDifficulty(r.Messages); d.RequiredTier != 1 {
		t.Fatal("history affected difficulty")
	}
}

type routingRiskDetector struct{ analysis services.RiskAnalysis }

func (r routingRiskDetector) AnalyzeText(string) services.RiskAnalysis { return r.analysis }

type routingChatMessages struct{ services.ChatMessageService }

func (routingChatMessages) RecordMessages(string, string, *string, *int, *int, *int, []services.PersistedChatMessage) error {
	return nil
}

func routingGateway(items []models.LLMModel, audit *stubAuditEventService) *service {
	return &service{
		modelRepo: &stubLLMModelRepository{active: items}, auditEventService: audit,
		chatSessionService: &stubChatSessionService{}, chatMessageService: routingChatMessages{},
		riskDetector: routingRiskDetector{}, riskHitService: services.NewRiskHitService(nil),
		invocationService: &stubModelInvocationService{}, secretRefService: services.NewSecretRefService(),
	}
}

func TestPrepareContentRoutingAuditAndFailover(t *testing.T) {
	m := routingModels()
	m[0].ModelGroup = "advanced"
	lowContext := m[1]
	lowContext.ID = 3
	lowContext.ContextWindowTokens = 1
	m = append(m, lowContext)
	audit := &stubAuditEventService{}
	gateway := routingGateway(m, audit)
	r := routingRequest("证明这个定理")
	prepared, err := gateway.prepareChatRequest(t.Context(), 1, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.deploymentCandidates) != 1 || prepared.deploymentCandidates[0].ID != 2 {
		t.Fatalf("unsafe failover candidates: %+v", prepared.deploymentCandidates)
	}
	found := false
	for _, event := range audit.events {
		if event.EventType != "gateway.routing.selected" {
			continue
		}
		found = true
		if event.Details == nil || strings.Contains(*event.Details, "证明这个定理") {
			t.Fatal("routing audit missing or contains prompt")
		}
		var d routingDecision
		if err := json.Unmarshal([]byte(*event.Details), &d); err != nil || d.ModelID != 2 || d.RequiredTier != 4 {
			t.Fatalf("bad decision: %+v %v", d, err)
		}
	}
	if !found {
		t.Fatal("routing audit missing")
	}
}

func TestAutoRoutingFallsBackAcrossHealthyModelGroups(t *testing.T) {
	items := routingModels()
	audit := &stubAuditEventService{}
	gateway := routingGateway(items, audit)
	now := time.Now().UTC().UnixMilli()
	gateway.deploymentStore = &routingDeploymentStore{snapshots: map[int]services.GatewayDeploymentSnapshot{
		1: {DeploymentID: 1, State: "cooldown", CooldownUntilMs: now + 60_000, Samples: 10, LastFailureMs: now},
		2: {DeploymentID: 2, State: "healthy", EWMATTFTMs: 200, EWMATokensPerSecond: 40, Samples: 10, LastSuccessMs: now},
	}}

	prepared, err := gateway.prepareChatRequest(t.Context(), 1, routingRequest("你好"))
	if err != nil {
		t.Fatalf("prepare auto route: %v", err)
	}
	if prepared.resolvedModel.ID != 2 || len(prepared.deploymentCandidates) != 1 || prepared.deploymentCandidates[0].ID != 2 {
		t.Fatalf("healthy cross-group fallback not selected: %+v", prepared.deploymentCandidates)
	}
	var selected routingDecision
	for _, event := range audit.events {
		if event.EventType == "gateway.routing.selected" && event.Details != nil {
			if err := json.Unmarshal([]byte(*event.Details), &selected); err != nil {
				t.Fatal(err)
			}
		}
	}
	if selected.ModelID != 2 || selected.ModelGroup != "advanced" || !slices.Contains(selected.Warnings, "primary_group_unavailable_health_fallback") {
		t.Fatalf("routing audit did not reflect health fallback: %+v", selected)
	}
}

func TestPrepareExplicitModelAndRiskPolicy(t *testing.T) {
	m := routingModels()
	audit := &stubAuditEventService{}
	gateway := routingGateway(m, audit)
	r := routingRequest("你好")
	r.Model = "capable"
	p, err := gateway.prepareChatRequest(t.Context(), 1, r)
	if err != nil || p.resolvedModel.ID != 2 {
		t.Fatalf("explicit model changed: %+v %v", p, err)
	}
	for _, e := range audit.events {
		if e.EventType == "gateway.routing.selected" {
			t.Fatal("explicit model was auto routed")
		}
	}
	gateway.riskDetector = routingRiskDetector{services.RiskAnalysis{IsSensitive: true, HighestAction: models.RiskActionBlock}}
	r.Model = "auto"
	if _, err := gateway.prepareChatRequest(t.Context(), 1, r); err == nil {
		t.Fatal("risk block bypassed")
	}
	m[0].IsSecure = false
	gateway = routingGateway(m, &stubAuditEventService{})
	gateway.riskDetector = routingRiskDetector{services.RiskAnalysis{IsSensitive: true, HighestAction: models.RiskActionRouteSecureModel}}
	r.Model = "cheap"
	r.AllowedModelGroups = []string{"basic"}
	if _, err := gateway.prepareChatRequest(t.Context(), 1, r); err == nil {
		t.Fatal("secure reroute escaped key allowlist")
	}
	r.Model = "auto"
	r.AllowedModelGroups = []string{"advanced"}
	if p, err = gateway.prepareChatRequest(t.Context(), 1, r); err != nil || p.resolvedModel.ID != 2 {
		t.Fatalf("secure-only key failed: %+v %v", p, err)
	}
}

func TestContentRoutingUsedByStreamingAndNonStreaming(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonstream", true: "stream"}[stream], func(t *testing.T) {
			var requested string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				requested = payload.Model
				// Terminal response avoids unrelated usage persistence in this test.
				http.Error(w, "test response", http.StatusBadRequest)
			}))
			defer upstream.Close()
			m := routingModels()
			for i := range m {
				m[i].BaseURL = upstream.URL
				m[i].ProviderType = models.ProviderTypeOpenAICompatible
				m[i].ProviderModelName = m[i].DisplayName
			}
			audit := &stubAuditEventService{}
			gateway := routingGateway(m, audit)
			gateway.httpClient, gateway.streamHTTPClient = upstream.Client(), upstream.Client()
			r := routingRequest("你好")
			r.Stream = stream
			var err error
			if stream {
				_, err = gateway.StreamChatCompletions(t.Context(), 1, r, httptest.NewRecorder())
			} else {
				_, _, err = gateway.ChatCompletions(t.Context(), 1, r)
			}
			if err != nil || requested != "cheap" {
				t.Fatalf("provider model %q, error %v", requested, err)
			}
		})
	}
}
