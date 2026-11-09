package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"clawreef/internal/models"
)

type reasoningLLMModelRepository struct {
	active []models.LLMModel
	byID   *models.LLMModel
	saved  *models.LLMModel
}

func (r *reasoningLLMModelRepository) List() ([]models.LLMModel, error) {
	return append([]models.LLMModel(nil), r.active...), nil
}
func (r *reasoningLLMModelRepository) ListActive() ([]models.LLMModel, error) {
	return append([]models.LLMModel(nil), r.active...), nil
}
func (r *reasoningLLMModelRepository) GetByID(int) (*models.LLMModel, error) { return r.byID, nil }
func (r *reasoningLLMModelRepository) GetByDisplayName(string) (*models.LLMModel, error) {
	return nil, nil
}
func (r *reasoningLLMModelRepository) Save(model *models.LLMModel) error {
	clone := *model
	r.saved = &clone
	return nil
}
func (r *reasoningLLMModelRepository) Delete(int) error { return nil }

func boolPointer(value bool) *bool { return &value }

func validReasoningModelRequest() SaveLLMModelRequest {
	return SaveLLMModelRequest{
		DisplayName:       "DeepSeek V4 Flash",
		ProviderType:      models.ProviderTypeOpenAICompatible,
		ProtocolType:      models.ProtocolTypeOpenAICompatible,
		BaseURL:           "https://api.deepseek.com",
		ProviderModelName: "deepseek-v4-flash",
		ProviderModels:    []models.LLMProviderModel{{ID: "deepseek-v4-flash"}},
		IsActive:          true,
	}
}

func TestSaveLLMModelPersistsCompleteDiscoveredCatalog(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	service := NewLLMModelService(repo)
	request := validReasoningModelRequest()
	request.ProviderModels = []models.LLMProviderModel{
		{ID: "qwen3.8"},
		{ID: "glm-5.2", DisplayName: "GLM 5.2"},
		{ID: "minimax-m3"},
	}
	request.ProviderModelName = "qwen3.8"

	saved, err := service.SaveModel(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.ProviderModels) != 3 || saved.ProviderModels[1].ID != "glm-5.2" {
		t.Fatalf("provider model catalog was not persisted: %#v", saved.ProviderModels)
	}
	if saved.ProviderModelsJSON == nil || *saved.ProviderModelsJSON == "" {
		t.Fatal("provider model catalog JSON was not populated")
	}
}

func TestSaveLLMModelPersistsSupportedReasoningChoice(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	service := NewLLMModelService(repo)
	request := validReasoningModelRequest()
	request.ReasoningEnabled = boolPointer(true)

	saved, err := service.SaveModel(request)
	if err != nil {
		t.Fatalf("SaveModel returned error: %v", err)
	}
	if !saved.SupportsReasoning || saved.ReasoningControl != models.ReasoningControlDeepSeekThinking {
		t.Fatalf("reasoning capability was not populated: %#v", saved)
	}
	if repo.saved == nil || !repo.saved.ReasoningEnabled {
		t.Fatalf("reasoning choice was not persisted: %#v", repo.saved)
	}
}

func TestSaveLLMModelRejectsExplicitUnsupportedReasoningEnable(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	service := NewLLMModelService(repo)
	request := validReasoningModelRequest()
	request.BaseURL = "https://gateway.example.com/v1"
	request.ReasoningEnabled = boolPointer(true)

	_, err := service.SaveModel(request)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported reasoning error, got %v", err)
	}
	if repo.saved != nil {
		t.Fatalf("unsupported reasoning choice must not be persisted: %#v", repo.saved)
	}
}

func TestSaveLLMModelOldClientSafelyDisablesReasoningWhenProviderChanges(t *testing.T) {
	current := &models.LLMModel{ID: 17, ReasoningEnabled: true}
	repo := &reasoningLLMModelRepository{byID: current}
	service := NewLLMModelService(repo)
	request := validReasoningModelRequest()
	request.ID = current.ID
	request.BaseURL = "https://gateway.example.com/v1"
	request.ReasoningEnabled = nil

	saved, err := service.SaveModel(request)
	if err != nil {
		t.Fatalf("old client update must remain compatible: %v", err)
	}
	if saved.ReasoningEnabled || saved.SupportsReasoning {
		t.Fatalf("unsupported provider must safely disable reasoning: %#v", saved)
	}
}

func TestSaveLLMModelPersistsContextWindow(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	service := NewLLMModelService(repo)
	request := validReasoningModelRequest()
	request.ContextWindowTokens = 128000

	saved, err := service.SaveModel(request)
	if err != nil {
		t.Fatalf("SaveModel returned error: %v", err)
	}
	if saved.ContextWindowTokens != 128000 || repo.saved == nil || repo.saved.ContextWindowTokens != 128000 {
		t.Fatalf("context window was not persisted: %#v", saved)
	}
}

func TestSaveLLMModelRejectsInvalidContextWindow(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	service := NewLLMModelService(repo)
	request := validReasoningModelRequest()
	request.ContextWindowTokens = -1

	if _, err := service.SaveModel(request); err == nil || !strings.Contains(err.Error(), "context window") {
		t.Fatalf("expected context window validation error, got %v", err)
	}
	if repo.saved != nil {
		t.Fatalf("invalid context window must not be persisted: %#v", repo.saved)
	}
}

func TestDiscoverOpenAICompatibleModelsCollectsAllPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":      []map[string]string{{"id": "model-a"}},
				"next_page": "/v1/models?after=page-2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "model-b"}},
		})
	}))
	defer server.Close()

	service := NewLLMModelService(&reasoningLLMModelRepository{})
	models, err := service.DiscoverProviderModels(DiscoverLLMModelsRequest{
		ProviderType: models.ProviderTypeOpenAICompatible,
		BaseURL:      server.URL,
	})
	if err != nil {
		t.Fatalf("DiscoverProviderModels returned error: %v", err)
	}
	if len(models) != 2 || models[0].ID != "model-a" || models[1].ID != "model-b" {
		t.Fatalf("expected both pages, got %#v", models)
	}
}

func TestDiscoverOpenAICompatibleModelsSupportsModelsArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"model-a"},{"name":"model-b"}]}`))
	}))
	defer server.Close()

	service := NewLLMModelService(&reasoningLLMModelRepository{})
	items, err := service.DiscoverProviderModels(DiscoverLLMModelsRequest{
		ProviderType: models.ProviderTypeOpenAICompatible,
		BaseURL:      server.URL,
	})
	if err != nil {
		t.Fatalf("DiscoverProviderModels returned error: %v", err)
	}
	if len(items) != 2 || items[0].ID != "model-a" || items[1].ID != "model-b" {
		t.Fatalf("expected models array to be normalized, got %#v", items)
	}
}

func TestBuildProviderEndpointPreservesQueryParameters(t *testing.T) {
	endpoint, err := buildProviderEndpoint("https://provider.example/v1?tenant=abc", "v1", "models")
	if err != nil {
		t.Fatalf("buildProviderEndpoint returned error: %v", err)
	}
	if endpoint != "https://provider.example/v1/models?tenant=abc" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
}

func TestClassifyDiscoveredModelBuildsConservativeRoutingPlan(t *testing.T) {
	reasoning := classifyDiscoveredModel("deepseek-v4-pro-reasoner", "deepseek-v4-pro-reasoner")
	if reasoning.SuggestedPool != "reasoning" || reasoning.SuggestedQualityTier != 4 || !reasoning.ChatCapable {
		t.Fatalf("unexpected reasoning plan: %#v", reasoning)
	}

	unknown := classifyDiscoveredModel("vendor-model-x", "vendor-model-x")
	if unknown.SuggestedPool != "default" || unknown.SuggestedQualityTier != 0 || unknown.Confidence != 0 {
		t.Fatalf("unexpected unknown chat plan: %#v", unknown)
	}

	embedding := classifyDiscoveredModel("bge-m3-embedding", "bge-m3-embedding")
	if embedding.ChatCapable || embedding.SuggestedPool != "specialized" {
		t.Fatalf("specialized model must not enter chat pools: %#v", embedding)
	}
}

func TestClassifyDiscoveredModelMatchesDelimitedReasoningNames(t *testing.T) {
	for _, name := range []string{"o3", "openai/o3", "deepseek-r1"} {
		plan := classifyDiscoveredModel(name, name)
		if plan.SuggestedPool != "reasoning" || plan.SuggestedQualityTier != 4 {
			t.Fatalf("%s was not classified as reasoning: %#v", name, plan)
		}
	}
}

func TestEnrichDiscoveredModelUsesProviderMetadata(t *testing.T) {
	plan := enrichDiscoveredModel(classifyDiscoveredModel("vendor-chat", "vendor-chat"), 128000, "chat", []string{"tools", "vision", "reasoning"})
	if plan.ContextWindowTokens != 128000 || !plan.SupportsTools || !plan.SupportsVision || !plan.SupportsReasoning {
		t.Fatalf("provider capabilities were not normalized: %#v", plan)
	}
	if plan.SuggestedPool != "reasoning" || plan.SuggestedQualityTier != 4 {
		t.Fatalf("reasoning metadata should promote the routing plan: %#v", plan)
	}

	specialized := enrichDiscoveredModel(classifyDiscoveredModel("vendor-x", "vendor-x"), 0, "embedding", nil)
	if specialized.ChatCapable || specialized.SuggestedPool != "specialized" {
		t.Fatalf("non-chat provider metadata must remain out of chat pools: %#v", specialized)
	}
}

func TestResolveDiscoveryURLRejectsExternalOrigin(t *testing.T) {
	if _, ok := resolveDiscoveryURL("https://provider.example/v1/models", "https://attacker.example/steal"); ok {
		t.Fatal("external pagination URL must be rejected")
	}
	if resolved, ok := resolveDiscoveryURL("https://provider.example/v1/models", "/v1/models?after=2"); !ok || resolved != "https://provider.example/v1/models?after=2" {
		t.Fatalf("same-origin pagination URL was not resolved: %q, %v", resolved, ok)
	}
}

func TestListExpandedActiveModelsReturnsOnlyConfiguredDeployments(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/models" {
			t.Fatalf("discovery path = %q, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer provider-key" {
			t.Fatalf("discovery authorization header was not forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v4-flash"},{"id":"deepseek-v4-pro"},{"id":"deepseek-v4-flash-vision-exp"}]}`))
	}))
	defer server.Close()

	apiKey := "provider-key"
	repo := &reasoningLLMModelRepository{active: []models.LLMModel{
		{
			ID:                2,
			DisplayName:       "deepseek",
			ProviderType:      models.ProviderTypeOpenAICompatible,
			ProtocolType:      models.ProtocolTypeOpenAICompatible,
			BaseURL:           server.URL,
			ProviderModelName: "deepseek-v4-flash",
			APIKey:            &apiKey,
			IsActive:          true,
		},
		{
			ID:                3,
			DisplayName:       "icompify",
			ProviderType:      models.ProviderTypeOpenAICompatible,
			ProtocolType:      models.ProtocolTypeOpenAICompatible,
			BaseURL:           server.URL,
			ProviderModelName: "deepseek-v4-flash-vision-exp",
			APIKey:            &apiKey,
			IsActive:          true,
		},
	}}
	service := NewLLMModelService(repo)

	items, err := service.ListExpandedActiveModels()
	if err != nil {
		t.Fatalf("ListExpandedActiveModels returned error: %v", err)
	}
	if len(items) != 2 || items[0].DisplayName != "deepseek" || items[1].DisplayName != "icompify" { t.Fatalf("configured deployments changed: %#v", items) }
	if _, err := service.ListExpandedActiveModels(); err != nil { t.Fatal(err) }
	if requests.Load() != 0 { t.Fatalf("runtime catalog must not discover/import models: %d", requests.Load()) }
}

func TestListExpandedActiveModelsFallsBackToConfiguredModelsWhenDiscoveryFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	repo := &reasoningLLMModelRepository{active: []models.LLMModel{{
		ID:                7,
		DisplayName:       "fallback-model",
		ProviderType:      models.ProviderTypeOpenAICompatible,
		ProtocolType:      models.ProtocolTypeOpenAICompatible,
		BaseURL:           server.URL,
		ProviderModelName: "provider-fallback-model",
		IsActive:          true,
	}}}
	service := NewLLMModelService(repo)

	items, err := service.ListExpandedActiveModels()
	if err != nil {
		t.Fatalf("temporary discovery failure must not fail the configured catalog: %v", err)
	}
	if len(items) != 1 || items[0].DisplayName != "fallback-model" {
		t.Fatalf("configured fallback model was not preserved: %#v", items)
	}
}
