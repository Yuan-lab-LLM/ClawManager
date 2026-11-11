package services

import (
	"clawreef/internal/models"
	"encoding/json"
	"slices"
	"testing"
)

func TestBuildOpenCodeGatewayConfigGroupsQualifiedModels(t *testing.T) {
	raw, err := buildOpenCodeGatewayConfig(`[
		"auto/auto",
		"deepseek/deepseek",
		"deepseek/deepseek-v4-pro",
		"modellist/org/model",
		"deepseek/deepseek-v4-pro"
	]`)
	if err != nil {
		t.Fatalf("buildOpenCodeGatewayConfig() error = %v", err)
	}

	var config openCodeGatewayConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("OpenCode config is not valid JSON: %v", err)
	}
	if config.Model != managedAutoModelRef {
		t.Fatalf("model = %q, want %q", config.Model, managedAutoModelRef)
	}
	if !slices.Equal(config.EnabledProviders, []string{"auto", "deepseek", "modellist"}) {
		t.Fatalf("enabled providers = %#v", config.EnabledProviders)
	}
	if slices.Contains(config.DisabledProviders, "deepseek") {
		t.Fatalf("configured provider deepseek remained disabled: %#v", config.DisabledProviders)
	}
	if got := config.Providers["deepseek"].Models; len(got) != 2 || got["deepseek-v4-pro"].Name != "deepseek-v4-pro" {
		t.Fatalf("deepseek models = %#v", got)
	}
	if got := config.Providers["modellist"].Models; got["org/model"].Name != "org/model" {
		t.Fatalf("model id containing slash was not preserved: %#v", got)
	}
	for providerID, provider := range config.Providers {
		if provider.Options.BaseURL != "{env:CLAWMANAGER_LLM_BASE_URL}" || provider.Options.APIKey != "{env:CLAWMANAGER_LLM_API_KEY}" {
			t.Fatalf("provider %q bypasses managed gateway env: %#v", providerID, provider.Options)
		}
	}
}

func TestBuildOpenCodeGatewayConfigDefaultsEmptyCatalogToAuto(t *testing.T) {
	raw, err := buildOpenCodeGatewayConfig(`[]`)
	if err != nil {
		t.Fatalf("buildOpenCodeGatewayConfig() error = %v", err)
	}
	var config openCodeGatewayConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	if config.Model != managedAutoModelRef || len(config.Providers) != 1 || config.Providers["auto"].Models["auto"].Name != "auto" {
		t.Fatalf("empty catalog fallback = %#v", config)
	}
}

func TestBuildOpenCodeGatewayConfigRejectsInvalidCatalog(t *testing.T) {
	if _, err := buildOpenCodeGatewayConfig(`not-json`); err == nil {
		t.Fatal("buildOpenCodeGatewayConfig() error = nil, want invalid catalogue error")
	}
}

func TestProviderCatalogInjectedForVisibleOwner(t *testing.T) {
	public := models.LLMModel{ID: 2, DisplayName: "icompify", ModelGroup: "icompify", ProviderModelName: "deepseek-flash", IsActive: true, VisibilityScope: "all"}
	private := models.LLMModel{ID: 3, DisplayName: "private", ProviderModelName: "private-model", IsActive: true, VisibilityScope: "selected_users", AllowedUserIDs: []int{1}}
	if err := models.SetLLMProviderModels(&public, []models.LLMProviderModel{{ID: "deepseek-flash"}, {ID: "glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	if err := models.SetLLMProviderModels(&private, []models.LLMProviderModel{{ID: "secret-model"}}); err != nil {
		t.Fatal(err)
	}
	s := &instanceService{llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{public, private}}}
	got, err := s.resolveGatewayModelInjection(2)
	if err != nil {
		t.Fatal(err)
	}
	var names, refs []string
	if err = json.Unmarshal([]byte(got.modelsJSON), &names); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(got.providerModelsJSON), &refs); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"auto", "deepseek-flash", "glm-5.3"}) {
		t.Fatalf("incomplete saved catalog or private model leak: %v", names)
	}
	if !slices.Equal(refs, []string{"auto/auto", "clawmanager/deepseek-flash", "clawmanager/glm-5.3"}) {
		t.Fatalf("provider grouping lost: %v", refs)
	}
}

func TestGatewayInjectionUsesRealModelIdentityAndConservativeDuplicates(t *testing.T) {
	items := []models.LLMModel{
		{ID: 1, DisplayName: "deepseek-v4.1-flash (epai)", ProviderModelName: "org/deepseek-v4.1-flash", ContextWindowTokens: 128000, SupportsVision: true, CapabilitySource: "provider_metadata", ProviderType: models.ProviderTypeOpenAICompatible},
		{ID: 2, DisplayName: "deepseek second deployment", ProviderModelName: "org/deepseek-v4.1-flash", ContextWindowTokens: 64000},
	}
	s := &instanceService{llmModelRepo: &stubLLMModelRepository{active: items}}
	got, err := s.resolveGatewayModelInjection(2)
	if err != nil {
		t.Fatal(err)
	}
	if got.modelsJSON != `["auto","org/deepseek-v4.1-flash"]` || got.providerModelsJSON != `["auto/auto","clawmanager/org/deepseek-v4.1-flash"]` || got.codingAgentDefaultModel != "org/deepseek-v4.1-flash" {
		t.Fatalf("display identity leaked into runtime: %+v", got)
	}
	var windows map[string]int
	var vision map[string]bool
	if err := json.Unmarshal([]byte(got.contextWindowsJSON), &windows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got.visionJSON), &vision); err != nil {
		t.Fatal(err)
	}
	if windows["org/deepseek-v4.1-flash"] != 64000 || vision["org/deepseek-v4.1-flash"] {
		t.Fatalf("duplicate deployments advertised unsafe capability: %s %s", got.contextWindowsJSON, got.visionJSON)
	}
}

func TestGatewayInjectionPreservesCaseSensitiveRealModelIDs(t *testing.T) {
	s := &instanceService{llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{
		{DisplayName: "first", ProviderModelName: "org/Model"},
		{DisplayName: "second", ProviderModelName: "org/model"},
	}}}
	got, err := s.resolveGatewayModelInjection(2)
	if err != nil {
		t.Fatal(err)
	}
	if got.providerModelsJSON != `["auto/auto","clawmanager/org/Model","clawmanager/org/model"]` {
		t.Fatalf("distinct upstream IDs collapsed: %s", got.providerModelsJSON)
	}
}
