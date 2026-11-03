package aigateway

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

func TestGenerationLimitsMatchAccountingAndWire(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		want, count  int
		invalid      bool
	}{
		{"default", "", 4096, 1, false},
		{"legacy", `,"max_tokens":8000`, 8000, 1, false},
		{"modern", `,"max_completion_tokens":32000`, 32000, 1, false},
		{"both_equal", `,"max_tokens":9000,"max_completion_tokens":9000`, 9000, 1, false},
		{"count", `,"max_completion_tokens":9000,"n":3`, 9000, 3, false},
		{"legacy_precedence", `,"max_tokens":100,"max_completion_tokens":200`, 100, 1, false},
		{"zero", `,"max_tokens":0`, 0, 0, true},
		{"negative", `,"max_completion_tokens":-1`, 0, 0, true},
		{"fraction", `,"max_tokens":1.5`, 0, 0, true},
		{"zero_count", `,"n":0`, 0, 0, true},
		{"overflow", `,"max_tokens":2147483647,"n":2`, 0, 0, true},
		{"huge_count", `,"n":9223372036854775807`, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ChatCompletionRequest{RawBody: []byte(`{"model":"auto","messages":[{"role":"user","content":"你好"}]` + tc.fields + `}`)}
			err := NormalizeGenerationLimits(&r)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid limits accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			input, output := routingTokenEstimate(r)
			if output != tc.want*tc.count || EstimateRequestTokens(r) != input+output {
				t.Fatalf("incorrect accounting: %d/%d", input, output)
			}
			for _, protocol := range []string{models.ProtocolTypeOpenAICompatible, models.ProtocolTypeAnthropic} {
				if tc.count > 1 && protocol == models.ProtocolTypeAnthropic {
					continue
				}
				model := &models.LLMModel{ProviderType: protocol, ProviderModelName: "test", InputPrice: 1, OutputPrice: 2}
				body, err := buildProviderRequestBody(r, model)
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatal(err)
				}
				limit := payload["max_tokens"]
				if limit == nil {
					limit = payload["max_completion_tokens"]
				}
				if limit != float64(tc.want) {
					t.Fatalf("%s wire limit = %v, want %d", protocol, limit, tc.want)
				}
				if got := estimateGatewayBudgetCost(r, []*models.LLMModel{model}); got != calculateEstimatedCost(model, input, output) {
					t.Fatal("budget estimator differs from router")
				}
			}
		})
	}
}

func TestAccountingIncludesToolsAndCJK(t *testing.T) {
	r := routingRequest("你好")
	base := EstimateRequestTokens(r)
	r.Tools = json.RawMessage(`[{"type":"function","function":{"name":"tool","description":"` + strings.Repeat("中文", 1000) + `"}}]`)
	input, output := routingTokenEstimate(r)
	if EstimateRequestTokens(r) < base+2000 {
		t.Fatal("CJK tools were omitted or substantially underestimated")
	}
	m := &models.LLMModel{InputPrice: 1, OutputPrice: 1}
	if math.Abs(estimateGatewayBudgetCost(r, []*models.LLMModel{m})-float64(input+output)/1e6) > 1e-12 {
		t.Fatal("budget omits tool tokens")
	}
	p, c, total, estimated := resolveRequestUsage(r, "你好", 0, 0, 0)
	if !estimated || p != input || c != estimateGatewayInputTokens("你好") || total != p+c {
		t.Fatal("missing-usage settlement differs from accounting")
	}
	p, c, total, estimated = resolveRequestUsage(r, "你好", 12, 3, 15)
	if estimated || p != 12 || c != 3 || total != 15 {
		t.Fatal("provider-reported usage was replaced")
	}
}

type routingBudgetStore struct {
	services.GatewayBudgetService
	reserved  float64
	finalized float64
}

func (s *routingBudgetStore) Reserve(_ context.Context, key int, amount float64) (*services.GatewayBudgetLease, error) {
	s.reserved = amount
	return &services.GatewayBudgetLease{GatewayKeyID: key, ReservedCost: amount}, nil
}
func (s *routingBudgetStore) Finalize(_ context.Context, _ *services.GatewayBudgetLease, amount float64) error {
	s.finalized = amount
	return nil
}
func (*routingBudgetStore) Release(context.Context, *services.GatewayBudgetLease) error { return nil }
func (*routingBudgetStore) Usage(context.Context, int) (*repository.GatewayKeyBudgetUsage, error) {
	return nil, nil
}

func TestUnsupportedProtocolsAndCurrenciesAreExcluded(t *testing.T) {
	for _, protocol := range []string{models.ProviderTypeGoogle, models.ProviderTypeAzureOpenAI, "unknown"} {
		m := routingModels()
		m[0].ProviderType = protocol
		gateway := routingGateway(m, &stubAuditEventService{})
		got, d, err := gateway.selectContentModel(t.Context(), routingRequest("你好"), false)
		if err != nil || got.ID != 2 || !slices.Contains(d.Rejections[0].Reasons, "unsupported_protocol_or_completion_count") {
			t.Fatalf("unsupported %s selected: %+v %+v %v", protocol, got, d, err)
		}
	}
	m := routingModels()
	m[0].Currency = "CNY"
	gateway := routingGateway(m, &stubAuditEventService{})
	r := routingRequest("你好")
	r.BudgetController = services.NewGatewayBudgetController(&routingBudgetStore{}, 1)
	p, err := gateway.prepareChatRequest(t.Context(), 1, r)
	if err != nil || p.resolvedModel.ID != 2 {
		t.Fatalf("USD route not chosen: %+v %v", p, err)
	}
	r.Model = "cheap"
	if _, err := gateway.prepareChatRequest(t.Context(), 1, r); err != services.ErrGatewayBudgetCurrencyMismatch {
		t.Fatalf("explicit CNY escaped guard: %v", err)
	}
	// Unsupported transport in an explicitly named same-group fallback must be excluded.
	m = routingModels()
	m[0].ModelGroup = m[1].ModelGroup
	m[1].ProviderType = models.ProviderTypeGoogle
	gateway = routingGateway(m, &stubAuditEventService{})
	r.Model = "cheap"
	p, err = gateway.prepareChatRequest(t.Context(), 1, r)
	if err != nil || len(p.deploymentCandidates) != 1 || p.deploymentCandidates[0].ID != 1 {
		t.Fatalf("unsupported fallback retained: %+v %v", p, err)
	}
}

func TestNoCandidatesAndRiskBlocksHaveRoutingAudit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*ChatCompletionRequest, *service, []models.LLMModel)
		reason string
	}{
		{"empty", func(_ *ChatCompletionRequest, s *service, _ []models.LLMModel) {
			s.modelRepo = &stubLLMModelRepository{}
		}, ""},
		{"key", func(r *ChatCompletionRequest, _ *service, _ []models.LLMModel) {
			r.AllowedModelGroups = []string{"missing"}
		}, "key_not_allowed"},
		{"context", func(r *ChatCompletionRequest, _ *service, _ []models.LLMModel) {
			r.Messages[0].Content = strings.Repeat("中", 60000)
		}, "context_capacity_exceeded"},
		{"protocol", func(_ *ChatCompletionRequest, _ *service, m []models.LLMModel) {
			for i := range m {
				m[i].ProviderType = "google"
			}
		}, "unsupported_protocol_or_completion_count"},
		{"currency", func(r *ChatCompletionRequest, _ *service, m []models.LLMModel) {
			for i := range m {
				m[i].Currency = "CNY"
			}
			r.BudgetController = services.NewGatewayBudgetController(&routingBudgetStore{}, 1)
		}, "budget_currency_mismatch"},
		{"risk", func(_ *ChatCompletionRequest, s *service, _ []models.LLMModel) {
			s.riskDetector = routingRiskDetector{services.RiskAnalysis{IsSensitive: true, HighestAction: models.RiskActionBlock}}
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routingModels()
			audit := &stubAuditEventService{}
			gateway := routingGateway(m, audit)
			r := routingRequest("private-prompt-marker")
			tc.setup(&r, gateway, m)
			if _, err := gateway.prepareChatRequest(t.Context(), 1, r); err == nil {
				t.Fatal("request not rejected")
			}
			found := false
			for _, e := range audit.events {
				if e.EventType != "gateway.routing.rejected" {
					continue
				}
				found = true
				if e.Details == nil || strings.Contains(*e.Details, "private-prompt-marker") {
					t.Fatal("missing details or prompt leaked")
				}
				var d routingDecision
				if err := json.Unmarshal([]byte(*e.Details), &d); err != nil || d.FailureReason == "" {
					t.Fatalf("missing rejection details: %+v %v", d, err)
				}
				if tc.reason != "" && (len(d.Rejections) != 2 || !slices.Contains(d.Rejections[0].Reasons, tc.reason)) {
					t.Fatalf("wrong exclusions: %+v", d.Rejections)
				}
			}
			if !found {
				t.Fatal("rejection audit missing")
			}
		})
	}
}

func TestHTTPGenerationLimitAndBudgetAgree(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var wireLimit float64
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			wireLimit, _ = body["max_completion_tokens"].(float64)
			http.Error(w, "terminal test response", http.StatusBadRequest)
		}))
		m := routingModels()
		for i := range m {
			m[i].BaseURL = upstream.URL
			m[i].ProviderModelName = m[i].DisplayName
		}
		gateway := routingGateway(m, &stubAuditEventService{})
		gateway.httpClient = upstream.Client()
		gateway.streamHTTPClient = upstream.Client()
		r := routingRequest("你好")
		r.Stream = stream
		r.RawBody = []byte(`{"model":"auto","messages":[{"role":"user","content":"你好"}],"max_completion_tokens":32000}`)
		store := &routingBudgetStore{}
		r.BudgetController = services.NewGatewayBudgetController(store, 1)
		var err error
		if stream {
			_, err = gateway.StreamChatCompletions(t.Context(), 1, r, httptest.NewRecorder())
		} else {
			_, _, err = gateway.ChatCompletions(t.Context(), 1, r)
		}
		upstream.Close()
		_, output := routingTokenEstimate(r)
		fallbacks := []*models.LLMModel{&m[0], &m[1]}
		if err != nil || wireLimit != 32000 || output != 32000 || store.reserved != estimateGatewayBudgetCost(r, fallbacks) {
			t.Fatalf("stream=%v limit=%v reserved=%v error=%v", stream, wireLimit, store.reserved, err)
		}
	}
}
