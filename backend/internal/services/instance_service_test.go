package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

func TestGatewayTokenAliasTTLConfig(t *testing.T) {
	t.Setenv(gatewayTokenAliasTTLEnv, "")
	if got := gatewayTokenAliasTTL(); got != defaultGatewayTokenAliasTTL {
		t.Fatalf("default gateway token alias TTL = %v, want %v", got, defaultGatewayTokenAliasTTL)
	}

	t.Setenv(gatewayTokenAliasTTLEnv, "2")
	if got := gatewayTokenAliasTTL(); got != 2*time.Hour {
		t.Fatalf("configured gateway token alias TTL = %v, want 2h", got)
	}

	t.Setenv(gatewayTokenAliasTTLEnv, "0")
	if got := gatewayTokenAliasTTL(); got != 0 {
		t.Fatalf("disabled gateway token alias TTL = %v, want 0", got)
	}
}

type stubLLMModelRepository struct {
	active []models.LLMModel
}

type expandedModelCatalogStub struct {
	active []models.LLMModel
}

func (s *expandedModelCatalogStub) ListExpandedActiveModels() ([]models.LLMModel, error) {
	return append([]models.LLMModel(nil), s.active...), nil
}

func (r *stubLLMModelRepository) List() ([]models.LLMModel, error) {
	items := make([]models.LLMModel, len(r.active))
	copy(items, r.active)
	return items, nil
}

func (r *stubLLMModelRepository) ListActive() ([]models.LLMModel, error) {
	items := make([]models.LLMModel, len(r.active))
	copy(items, r.active)
	return items, nil
}

func (r *stubLLMModelRepository) GetByID(id int) (*models.LLMModel, error) {
	return nil, nil
}

func (r *stubLLMModelRepository) GetByDisplayName(displayName string) (*models.LLMModel, error) {
	return nil, nil
}

func (r *stubLLMModelRepository) Save(model *models.LLMModel) error {
	return nil
}

func (r *stubLLMModelRepository) Delete(id int) error {
	return nil
}

func TestBuildGatewayEnvInjectsGatewayModelCatalog(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example/api/v1/gateway/llm")

	token := "igt_test_token"
	for _, instanceType := range []string{"openclaw", "hermes", "opencode", "workbuddy", RuntimeTypeDeepSeekHarness} {
		t.Run(instanceType, func(t *testing.T) {
			service := &instanceService{
				llmModelRepo: &stubLLMModelRepository{
					active: []models.LLMModel{
						{DisplayName: "GPT-4.1", ModelGroup: "general", ContextWindowTokens: 128000},
						{DisplayName: "Claude 3.7 Sonnet", ModelGroup: "general", ContextWindowTokens: 200000},
						{DisplayName: "auto", ContextWindowTokens: 64000},
						{ProviderModelName: "deepseek-r1", ContextWindowTokens: 32000, ProviderType: models.ProviderTypeOpenAICompatible, ProtocolType: models.ProtocolTypeOpenAICompatible, BaseURL: "https://api.deepseek.com"},
					},
				},
			}

			env, err := service.buildGatewayEnv(&models.Instance{
				UserID:      1,
				Type:        instanceType,
				AccessToken: &token,
			})
			if err != nil {
				t.Fatalf("buildGatewayEnv returned error: %v", err)
			}

			if env["CLAWMANAGER_LLM_BASE_URL"] != "http://gateway.example/api/v1/gateway/llm" {
				t.Fatalf("expected CLAWMANAGER_LLM_BASE_URL to use gateway base URL, got %q", env["CLAWMANAGER_LLM_BASE_URL"])
			}
			if env["CLAWMANAGER_LLM_MODEL"] != `["auto","GPT-4.1","Claude 3.7 Sonnet","deepseek-r1"]` {
				t.Fatalf("expected CLAWMANAGER_LLM_MODEL to contain injected model catalog JSON, got %q", env["CLAWMANAGER_LLM_MODEL"])
			}
			if env["CLAWMANAGER_LLM_REASONING"] != `{"Claude 3.7 Sonnet":false,"Claude 3.7 Sonnet/Claude 3.7 Sonnet":false,"GPT-4.1":false,"GPT-4.1/GPT-4.1":false,"auto":false,"deepseek-r1":false,"openai-compatible/deepseek-r1":false}` {
				t.Fatalf("expected authoritative reasoning settings, got %q", env["CLAWMANAGER_LLM_REASONING"])
			}
			if env["CLAWMANAGER_LLM_REASONING_CONTROL"] != `{"Claude 3.7 Sonnet":"","Claude 3.7 Sonnet/Claude 3.7 Sonnet":"","GPT-4.1":"","GPT-4.1/GPT-4.1":"","auto":"","deepseek-r1":"deepseek-thinking","openai-compatible/deepseek-r1":"deepseek-thinking"}` {
				t.Fatalf("expected authoritative reasoning controls, got %q", env["CLAWMANAGER_LLM_REASONING_CONTROL"])
			}
			if env["CLAWMANAGER_LLM_PROVIDER_MODELS"] != `["auto/auto","GPT-4.1/GPT-4.1","Claude 3.7 Sonnet/Claude 3.7 Sonnet","openai-compatible/deepseek-r1"]` {
				t.Fatalf("expected CLAWMANAGER_LLM_PROVIDER_MODELS to preserve configured provider names, got %q", env["CLAWMANAGER_LLM_PROVIDER_MODELS"])
			}
			if env["OPENAI_MODEL"] != "auto" {
				t.Fatalf("expected OPENAI_MODEL to remain the default gateway alias, got %q", env["OPENAI_MODEL"])
			}
			if env["CLAWMANAGER_LLM_CONTEXT_WINDOW"] != "32000" {
				t.Fatalf("expected conservative default context window, got %q", env["CLAWMANAGER_LLM_CONTEXT_WINDOW"])
			}
			var contextWindows map[string]int
			if err := json.Unmarshal([]byte(env["CLAWMANAGER_LLM_CONTEXT_WINDOWS"]), &contextWindows); err != nil {
				t.Fatalf("context window catalogue is not valid JSON: %v", err)
			}
			if contextWindows["GPT-4.1"] != 128000 || contextWindows["Claude 3.7 Sonnet"] != 128000 || contextWindows["general"] != 128000 {
				t.Fatalf("unexpected context window catalogue: %#v", contextWindows)
			}
			if env["CLAWMANAGER_LLM_API_KEY"] != token || env["OPENAI_API_KEY"] != token {
				t.Fatalf("expected gateway token aliases to be preserved")
			}
			if instanceType == "opencode" {
				assertOpenCodeGatewayConfig(t, env["OPENCODE_CONFIG_CONTENT"])
			}
		})
	}
}

func TestResolveGatewayModelInjectionDoesNotGrantDiscoveredModels(t *testing.T) {
	provider := models.LLMModel{
		DisplayName:       "icompify",
		ProviderType:      models.ProviderTypeOpenAICompatible,
		ProtocolType:      models.ProtocolTypeOpenAICompatible,
		ProviderModelName: "qwen3.8",
		IsActive:          true,
	}
	if err := models.SetLLMProviderModels(&provider, []models.LLMProviderModel{
		{ID: "qwen3.8"},
		{ID: "glm-5.2"},
		{ID: "minimax-m3"},
	}); err != nil {
		t.Fatal(err)
	}
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{provider}},
	}

	injection, err := service.resolveGatewayModelInjection(1)
	if err != nil {
		t.Fatal(err)
	}
	if injection.modelsJSON != `["auto","icompify"]` {
		t.Fatalf("provider catalog was not expanded for instance injection: %s", injection.modelsJSON)
	}
	if injection.providerModelsJSON != `["auto/auto","icompify/icompify"]` {
		t.Fatalf("provider-qualified catalog was not injected: %s", injection.providerModelsJSON)
	}
	if injection.codingAgentDefaultModel != "icompify" {
		t.Fatalf("coding agent default = %q, want icompify", injection.codingAgentDefaultModel)
	}
}

func TestResolveGatewayModelInjectionIgnoresUnapprovedExpandedCatalog(t *testing.T) {
	expanded := []models.LLMModel{
		{DisplayName: "model-1", ProviderModelName: "provider-model-1", CatalogProviderName: "configured-provider"},
		{DisplayName: "model-2", ProviderModelName: "provider-model-2", CatalogProviderName: "configured-provider"},
		{DisplayName: "model-3", ProviderModelName: "provider-model-3", CatalogProviderName: "configured-provider"},
		{DisplayName: "model-4", ProviderModelName: "provider-model-4", CatalogProviderName: "configured-provider"},
		{DisplayName: "model-5", ProviderModelName: "provider-model-5", CatalogProviderName: "configured-provider"},
		{DisplayName: "model-6", ProviderModelName: "provider-model-6", CatalogProviderName: "configured-provider"},
		{DisplayName: "model-7", ProviderModelName: "provider-model-7", CatalogProviderName: "configured-provider"},
	}
	service := &instanceService{
		llmModelRepo:         &stubLLMModelRepository{active: expanded[:1]},
		expandedModelCatalog: &expandedModelCatalogStub{active: expanded},
	}

	injection, err := service.resolveGatewayModelInjection(1)
	if err != nil {
		t.Fatalf("resolveGatewayModelInjection returned error: %v", err)
	}
	if injection.modelsJSON != `["auto","model-1"]` {
		t.Fatalf("expanded provider models were not injected: %s", injection.modelsJSON)
	}
	if injection.providerModelsJSON != `["auto/auto","configured-provider/model-1"]` {
		t.Fatalf("expanded provider model references were not injected: %s", injection.providerModelsJSON)
	}
	if injection.codingAgentDefaultModel != "model-1" {
		t.Fatalf("coding agent default = %q, want model-1", injection.codingAgentDefaultModel)
	}
}

func assertOpenCodeGatewayConfig(t *testing.T, raw string) {
	t.Helper()
	var config struct {
		Model    string `json:"model"`
		Provider map[string]struct {
			NPM     string            `json:"npm"`
			Options map[string]string `json:"options"`
			Models  map[string]struct {
				Name  string `json:"name"`
				Limit struct {
					Context int `json:"context"`
				} `json:"limit"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT is not valid JSON: %v", err)
	}
	if config.Model != "clawmanager/auto" { t.Fatalf("legacy provider reference changed: %s", raw) }
	provider, ok := config.Provider["clawmanager"]
	if !ok || provider.NPM != "@ai-sdk/openai-compatible" || provider.Options["baseURL"] != "{env:CLAWMANAGER_LLM_BASE_URL}" || provider.Options["apiKey"] != "{env:CLAWMANAGER_LLM_API_KEY}" { t.Fatalf("missing governed provider: %s", raw) }
	for _, name := range []string{"auto","GPT-4.1","Claude 3.7 Sonnet","deepseek-r1"} {
		if _, ok := provider.Models[name]; !ok { t.Fatalf("missing model %q", name) }
	}
	if provider.Models["auto"].Limit.Context != 32000 || provider.Models["GPT-4.1"].Limit.Context != 128000 || provider.Models["Claude 3.7 Sonnet"].Limit.Context != 128000 {
		t.Fatalf("OpenCode config is missing model context limits: %s", raw)
	}
}

func TestResolveGatewayModelInjectionKeepsUnknownAggregateContextWindow(t *testing.T) {
	service := &instanceService{llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{
		{DisplayName: "known", ModelGroup: "shared", ContextWindowTokens: 128000},
		{DisplayName: "unknown", ModelGroup: "shared", ContextWindowTokens: 0},
	}}}

	injection, err := service.resolveGatewayModelInjection(1)
	if err != nil {
		t.Fatalf("resolveGatewayModelInjection returned error: %v", err)
	}
	if injection.defaultContextWindow != 0 {
		t.Fatalf("default context window = %d, want unknown", injection.defaultContextWindow)
	}
	var contextWindows map[string]int
	if err := json.Unmarshal([]byte(injection.contextWindowsJSON), &contextWindows); err != nil {
		t.Fatalf("context window catalogue is not valid JSON: %v", err)
	}
	if contextWindows["shared"] != 0 || contextWindows["known"] != 0 || contextWindows["unknown"] != 0 {
		t.Fatalf("unexpected context window aggregation: %#v", contextWindows)
	}
}

func TestBuildGatewayEnvEnsuresMissingGatewayToken(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example/api/v1/gateway/llm")

	instanceRepo := &stubGatewayEnvInstanceRepository{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo()}
	service := &instanceService{
		instanceRepo: instanceRepo,
		llmModelRepo: &stubLLMModelRepository{
			active: []models.LLMModel{{DisplayName: "auto"}},
		},
	}
	instance := &models.Instance{
		ID:     68,
		UserID: 1,
		Type:   "openclaw",
	}

	env, err := service.BuildGatewayEnv(instance)
	if err != nil {
		t.Fatalf("BuildGatewayEnv returned error: %v", err)
	}
	if instance.AccessToken == nil || *instance.AccessToken == "" {
		t.Fatal("BuildGatewayEnv did not provision instance access token")
	}
	if env["CLAWMANAGER_LLM_API_KEY"] != *instance.AccessToken {
		t.Fatalf("CLAWMANAGER_LLM_API_KEY = %q, want provisioned token %q", env["CLAWMANAGER_LLM_API_KEY"], *instance.AccessToken)
	}
	if got := instanceRepo.updated[68]; got == nil || got.AccessToken == nil || *got.AccessToken != *instance.AccessToken {
		t.Fatalf("repository update did not persist provisioned token: %#v", instanceRepo.updated[68])
	}
	alias, ok := instanceRepo.aliases[68]
	if !ok {
		t.Fatal("repository did not record provisioned token as a gateway alias")
	}
	if alias.token != *instance.AccessToken {
		t.Fatalf("gateway alias token = %q, want provisioned token %q", alias.token, *instance.AccessToken)
	}
	if alias.expiresAt.Before(time.Now().Add(6 * 24 * time.Hour)) {
		t.Fatalf("gateway alias expiry = %v, want roughly the default compatibility window", alias.expiresAt)
	}
}

func TestBuildGatewayEnvRecordsExistingGatewayTokenAlias(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example/api/v1/gateway/llm")

	token := "igt_existing_token"
	instanceRepo := &stubGatewayEnvInstanceRepository{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo()}
	service := &instanceService{
		instanceRepo: instanceRepo,
		llmModelRepo: &stubLLMModelRepository{
			active: []models.LLMModel{{DisplayName: "auto"}},
		},
	}

	env, err := service.BuildGatewayEnv(&models.Instance{
		ID:          69,
		UserID:      1,
		Type:        "hermes",
		AccessToken: &token,
	})
	if err != nil {
		t.Fatalf("BuildGatewayEnv returned error: %v", err)
	}
	if env["CLAWMANAGER_LLM_API_KEY"] != token {
		t.Fatalf("CLAWMANAGER_LLM_API_KEY = %q, want existing token", env["CLAWMANAGER_LLM_API_KEY"])
	}
	alias, ok := instanceRepo.aliases[69]
	if !ok {
		t.Fatal("repository did not record existing token as a gateway alias")
	}
	if alias.token != token {
		t.Fatalf("gateway alias token = %q, want existing token %q", alias.token, token)
	}
}
func TestBuildGatewayEnvMergesEnvironmentOverrides(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example/api/v1/gateway/llm")
	token := "igt_test_token"
	raw, err := marshalEnvironmentOverrides(map[string]string{
		"CLAWMANAGER_TEAM_ENABLED":   "true",
		"CLAWMANAGER_TEAM_MEMBER_ID": "lite-worker",
		"CUSTOM_GATEWAY_ENV":         "enabled",
	})
	if err != nil {
		t.Fatalf("marshalEnvironmentOverrides returned error: %v", err)
	}
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{
			active: []models.LLMModel{{DisplayName: "auto"}},
		},
	}

	env, err := service.BuildGatewayEnv(&models.Instance{
		ID:                       88,
		UserID:                   1,
		Type:                     "openclaw",
		RuntimeType:              RuntimeBackendGateway,
		AccessToken:              &token,
		EnvironmentOverridesJSON: raw,
	})
	if err != nil {
		t.Fatalf("BuildGatewayEnv returned error: %v", err)
	}

	if env["CLAWMANAGER_TEAM_ENABLED"] != "true" || env["CLAWMANAGER_TEAM_MEMBER_ID"] != "lite-worker" {
		t.Fatalf("expected Team environment overrides to be merged into gateway env, got %#v", env)
	}
	if env["CUSTOM_GATEWAY_ENV"] != "enabled" {
		t.Fatalf("expected custom gateway environment override to be merged, got %#v", env)
	}
	if env["CLAWMANAGER_LLM_API_KEY"] != token {
		t.Fatalf("expected gateway token env to remain available")
	}
	if env["CLAWMANAGER_RUNTIME_TYPE"] != RuntimeBackendGateway {
		t.Fatalf("expected runtime type marker %q, got %q", RuntimeBackendGateway, env["CLAWMANAGER_RUNTIME_TYPE"])
	}
}

func TestBuildGatewayEnvSkipsUnmanagedRuntime(t *testing.T) {
	token := "igt_test_token"
	service := &instanceService{}

	env, err := service.buildGatewayEnv(&models.Instance{
		Type:        "ubuntu",
		AccessToken: &token,
	})
	if err != nil {
		t.Fatalf("buildGatewayEnv returned error: %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("expected unmanaged runtime to receive no gateway env, got %#v", env)
	}
}

type gatewayTokenAliasRecord struct {
	token     string
	expiresAt time.Time
}

type stubGatewayEnvInstanceRepository struct {
	*fakeRuntimeInstanceRepo
	updated map[int]*models.Instance
	aliases map[int]gatewayTokenAliasRecord
}

func (r *stubGatewayEnvInstanceRepository) Update(instance *models.Instance) error {
	if r.updated == nil {
		r.updated = map[int]*models.Instance{}
	}
	copy := *instance
	r.updated[instance.ID] = &copy
	return nil
}

func (r *stubGatewayEnvInstanceRepository) UpsertGatewayTokenAlias(ctx context.Context, instanceID int, accessToken string, expiresAt time.Time) error {
	if r.aliases == nil {
		r.aliases = map[int]gatewayTokenAliasRecord{}
	}
	r.aliases[instanceID] = gatewayTokenAliasRecord{token: accessToken, expiresAt: expiresAt}
	return nil
}
func TestBuildAgentEnvInjectsHermesAgentConfig(t *testing.T) {
	t.Setenv("CLAWMANAGER_AGENT_CONTROL_BASE_URL", "http://agent-control.example")

	token := "agt_boot_test_token"
	service := &instanceService{}

	env, err := service.buildAgentEnv(&models.Instance{
		ID:                  24,
		Type:                "hermes",
		DiskGB:              20,
		AgentBootstrapToken: &token,
	})
	if err != nil {
		t.Fatalf("buildAgentEnv returned error: %v", err)
	}

	if env["CLAWMANAGER_AGENT_ENABLED"] != "true" {
		t.Fatalf("expected Hermes agent to be enabled")
	}
	if env["CLAWMANAGER_AGENT_BASE_URL"] != "http://agent-control.example" {
		t.Fatalf("expected Hermes agent base URL to be injected, got %q", env["CLAWMANAGER_AGENT_BASE_URL"])
	}
	if env["CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN"] != token {
		t.Fatalf("expected Hermes agent bootstrap token to be injected")
	}
	if env["CLAWMANAGER_AGENT_INSTANCE_ID"] != "24" {
		t.Fatalf("expected Hermes instance id to be injected, got %q", env["CLAWMANAGER_AGENT_INSTANCE_ID"])
	}
	if env["CLAWMANAGER_AGENT_PERSISTENT_DIR"] != "/config/.hermes" {
		t.Fatalf("expected Hermes persistent dir /config/.hermes, got %q", env["CLAWMANAGER_AGENT_PERSISTENT_DIR"])
	}
	if env["CLAWMANAGER_AGENT_DISK_LIMIT_BYTES"] != "21474836480" {
		t.Fatalf("expected Hermes disk limit bytes to be injected, got %q", env["CLAWMANAGER_AGENT_DISK_LIMIT_BYTES"])
	}
}

func TestPersistentVolumeMountPathNormalizesManagedDesktopRuntimes(t *testing.T) {
	for _, instanceType := range []string{"openclaw", "ubuntu", "webtop", "hermes", "opencode", "workbuddy", RuntimeTypeDeepSeekHarness} {
		t.Run(instanceType, func(t *testing.T) {
			got := persistentVolumeMountPath(&models.Instance{
				Type:      instanceType,
				MountPath: "/data",
			})
			if got != "/config" {
				t.Fatalf("expected %s PVC mount path /config, got %q", instanceType, got)
			}
		})
	}
	got := persistentVolumeMountPath(&models.Instance{Type: "workbuddy", MountPath: "/storage"})
	if got != "/storage" {
		t.Fatalf("expected Workbuddy PVC mount path /storage, got %q", got)
	}
	got = persistentVolumeMountPath(&models.Instance{Type: RuntimeTypeCodex, MountPath: "/storage"})
	if got != "/storage" {
		t.Fatalf("expected Codex PVC mount path /storage, got %q", got)
	}
}

func TestManagedRuntimePersistentDirKeepsHermesSubdirectory(t *testing.T) {
	got := managedRuntimePersistentDir(&models.Instance{
		Type:      "hermes",
		MountPath: "/config",
	})
	if got != "/config/.hermes" {
		t.Fatalf("expected Hermes persistent dir /config/.hermes, got %q", got)
	}
}

func TestManagedRuntimePersistentDirKeepsDeepSeekHarnessSubdirectory(t *testing.T) {
	if got := managedRuntimePersistentDir(&models.Instance{Type: RuntimeTypeDeepSeekHarness, MountPath: "/config"}); got != "/config/.dsh" {
		t.Fatalf("DeepSeek Harness Pro persistent dir = %q", got)
	}
	workspace := "/workspaces/deepseek-harness/user-7/instance-11"
	if got := managedRuntimePersistentDir(&models.Instance{
		Type: RuntimeTypeDeepSeekHarness, InstanceMode: InstanceModeLite, RuntimeType: RuntimeBackendGateway, WorkspacePath: &workspace,
	}); got != workspace+"/home/.dsh" {
		t.Fatalf("DeepSeek Harness Lite persistent dir = %q", got)
	}
}

func TestWorkbuddySupportsManagedRuntimeInjection(t *testing.T) {
	if !supportsManagedRuntimeIntegration("workbuddy") {
		t.Fatal("expected Workbuddy to support managed runtime integration")
	}
	if !supportsRuntimeConfigInjection("workbuddy") {
		t.Fatal("expected Workbuddy to support runtime config injection")
	}

	t.Setenv("CLAWMANAGER_AGENT_CONTROL_BASE_URL", "http://agent-control.example")
	token := "agt_boot_workbuddy"
	env, err := (&instanceService{}).buildAgentEnv(&models.Instance{
		ID:                  923,
		Type:                "workbuddy",
		DiskGB:              20,
		MountPath:           "/config",
		AgentBootstrapToken: &token,
	})
	if err != nil {
		t.Fatalf("buildAgentEnv returned error: %v", err)
	}
	if env["CLAWMANAGER_AGENT_RUNTIME_TYPE"] != "workbuddy" {
		t.Fatalf("expected Workbuddy agent runtime type, got %q", env["CLAWMANAGER_AGENT_RUNTIME_TYPE"])
	}
	if env["CLAWMANAGER_AGENT_PERSISTENT_DIR"] != "/config" {
		t.Fatalf("expected Workbuddy agent persistent dir /config, got %q", env["CLAWMANAGER_AGENT_PERSISTENT_DIR"])
	}
}
func TestRuntimeVolumeInitScriptsAddsHermesLayoutMigration(t *testing.T) {
	scripts := runtimeVolumeInitScripts("hermes", "/config")
	if len(scripts) != 1 {
		t.Fatalf("expected one Hermes volume init script, got %d", len(scripts))
	}
	if scripts[0].Name != "data" || scripts[0].MountPath != "/config" {
		t.Fatalf("unexpected Hermes volume init script: %#v", scripts[0])
	}
	if !strings.Contains(scripts[0].Script, `target="$base/.hermes"`) {
		t.Fatalf("expected Hermes init script to target /config/.hermes, got %s", scripts[0].Script)
	}
}

func TestRuntimeVolumeInitScriptsConfiguresOpenCodeKonsoleShell(t *testing.T) {
	scripts := runtimeVolumeInitScripts("opencode", "/config")
	if len(scripts) != 1 {
		t.Fatalf("expected one OpenCode volume init script, got %d", len(scripts))
	}
	for _, expected := range []string{
		`.config/konsolerc`,
		`.local/share/konsole/ClawManager.profile`,
		`DefaultProfile=ClawManager.profile`,
		`Command=/bin/bash`,
	} {
		if !strings.Contains(scripts[0].Script, expected) {
			t.Fatalf("expected OpenCode init script to contain %q, got %s", expected, scripts[0].Script)
		}
	}
}

func TestResolveGatewayModelInjectionRequiresActiveModels(t *testing.T) {
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{},
	}

	injection, err := service.resolveGatewayModelInjection(1)
	if err == nil {
		t.Fatalf("expected resolveGatewayModelInjection to fail when no active models exist, got %#v", injection)
	}
}

func TestRenderWindowsCodexBootstrapFiles(t *testing.T) {
	files, err := renderWindowsCodexBootstrapFiles(
		"http://clawmanager-gateway.system.svc:9001/api/v1/gateway/llm",
		"gpt-5.4",
		"igt_instance_token",
	)
	if err != nil {
		t.Fatalf("renderWindowsCodexBootstrapFiles returned error: %v", err)
	}
	config := files[windowsCodexConfigKey]
	for _, expected := range []string{
		`model_provider = "clawmanager"`,
		`model = "gpt-5.4"`,
		`review_model = "gpt-5.4"`,
		`base_url = "http://clawmanager-gateway.system.svc:9001/api/v1/gateway/llm/v1"`,
		`wire_api = "responses"`,
		`requires_openai_auth = true`,
		`sandbox_mode = "danger-full-access"`,
		`approval_policy = "never"`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config missing %q:\n%s", expected, config)
		}
	}
	if strings.Contains(config, "igt_instance_token") {
		t.Fatal("config.toml must not contain the instance token")
	}
	if strings.Contains(config, "default_permissions") || strings.Contains(config, "[sandbox_workspace_write]") {
		t.Fatal("full-access sandbox config must not include a permission profile or workspace-write settings")
	}

	var auth map[string]string
	if err := json.Unmarshal([]byte(files[windowsCodexAuthKey]), &auth); err != nil {
		t.Fatalf("auth.json is invalid JSON: %v", err)
	}
	if got := auth["OPENAI_API_KEY"]; got != "igt_instance_token" {
		t.Fatalf("auth OPENAI_API_KEY = %q", got)
	}
}

func TestRenderWindowsCodexBootstrapFilesDoesNotDuplicateVersionPath(t *testing.T) {
	files, err := renderWindowsCodexBootstrapFiles(
		"http://clawmanager-gateway.system.svc:9001/api/v1/gateway/llm/v1/",
		"gpt-5.4",
		"igt_instance_token",
	)
	if err != nil {
		t.Fatalf("renderWindowsCodexBootstrapFiles returned error: %v", err)
	}

	config := files[windowsCodexConfigKey]
	if !strings.Contains(config, `base_url = "http://clawmanager-gateway.system.svc:9001/api/v1/gateway/llm/v1"`) {
		t.Fatalf("config has unexpected base URL:\n%s", config)
	}
}

func TestSecurityModeForInstance(t *testing.T) {
	service := &instanceService{}

	if got := service.securityModeForInstance("openclaw"); got != "chromium-compat" {
		t.Fatalf("expected openclaw to use chromium compat mode, got %q", got)
	}
	if got := service.securityModeForInstance("ubuntu"); got != "default" {
		t.Fatalf("expected ubuntu to use default security mode, got %q", got)
	}
	if got := service.securityModeForInstance("workbuddy"); got != "privileged" {
		t.Fatalf("expected Workbuddy to use privileged mode for KVM, got %q", got)
	}
	if got := service.securityModeForInstance(RuntimeTypeCodex); got != "privileged" {
		t.Fatalf("expected Codex to use privileged mode for KVM, got %q", got)
	}

	service.allowPrivilegedPods = true
	if got := service.securityModeForInstance("openclaw"); got != "privileged" {
		t.Fatalf("expected explicit privileged override to win, got %q", got)
	}
}

func TestSecurityModeForRuntimeUsesLinuxWorkbuddySandboxMode(t *testing.T) {
	service := &instanceService{}

	linuxWorkbuddy := &models.Instance{Type: "workbuddy", RuntimeVariant: WorkbuddyRuntimeLinux}
	if got := service.securityModeForRuntime(linuxWorkbuddy); got != k8s.PodSecurityWorkbuddyLinux {
		t.Fatalf("expected Linux WorkBuddy to use its bubblewrap-compatible mode, got %q", got)
	}

	windowsWorkbuddy := &models.Instance{Type: "workbuddy", RuntimeVariant: WorkbuddyRuntimeWindows}
	if got := service.securityModeForRuntime(windowsWorkbuddy); got != k8s.PodSecurityPrivileged {
		t.Fatalf("expected Windows WorkBuddy to retain the KVM privileged mode, got %q", got)
	}

	linuxCodex := &models.Instance{Type: RuntimeTypeCodex, RuntimeVariant: WorkbuddyRuntimeLinux}
	if got := service.securityModeForRuntime(linuxCodex); got != k8s.PodSecurityChromiumCompat {
		t.Fatalf("expected Linux Codex to retain chromium compat mode, got %q", got)
	}
}
