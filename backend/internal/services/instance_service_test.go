package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
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
	for _, instanceType := range []string{"openclaw", "hermes"} {
		t.Run(instanceType, func(t *testing.T) {
			service := &instanceService{
				llmModelRepo: &stubLLMModelRepository{
					active: []models.LLMModel{
						{DisplayName: "GPT-4.1"},
						{DisplayName: "Claude 3.7 Sonnet"},
						{DisplayName: "auto"},
						{ProviderModelName: "deepseek-r1"},
					},
				},
			}

			env, err := service.buildGatewayEnv(&models.Instance{
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
			if env["OPENAI_MODEL"] != "auto" {
				t.Fatalf("expected OPENAI_MODEL to remain the default gateway alias, got %q", env["OPENAI_MODEL"])
			}
			if env["CLAWMANAGER_LLM_API_KEY"] != token || env["OPENAI_API_KEY"] != token {
				t.Fatalf("expected gateway token aliases to be preserved")
			}
		})
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
	for _, instanceType := range []string{"openclaw", "ubuntu", "webtop", "hermes"} {
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

func TestResolveGatewayModelInjectionRequiresActiveModels(t *testing.T) {
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{},
	}

	injection, err := service.resolveGatewayModelInjection()
	if err == nil {
		t.Fatalf("expected resolveGatewayModelInjection to fail when no active models exist, got %#v", injection)
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

	service.allowPrivilegedPods = true
	if got := service.securityModeForInstance("openclaw"); got != "privileged" {
		t.Fatalf("expected explicit privileged override to win, got %q", got)
	}
}
