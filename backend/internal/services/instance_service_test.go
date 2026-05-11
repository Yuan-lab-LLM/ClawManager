package services

import (
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
)

type stubLLMModelRepository struct {
	active []models.LLMModel
	err    error
}

func (r *stubLLMModelRepository) List() ([]models.LLMModel, error) {
	if r.err != nil {
		return nil, r.err
	}
	items := make([]models.LLMModel, len(r.active))
	copy(items, r.active)
	return items, nil
}

func (r *stubLLMModelRepository) ListActive() ([]models.LLMModel, error) {
	if r.err != nil {
		return nil, r.err
	}
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

func TestAdditionalServicePortsForOpenClawFreshInstanceIncludesControlUI(t *testing.T) {
	ports := additionalServicePortsForInstance("openclaw", DefaultDesktopTargetPort)
	if len(ports) != 1 || ports[0] != DefaultControlUITargetPort {
		t.Fatalf("additionalServicePortsForInstance(openclaw, 3001) = %#v, want []int32{18789}", ports)
	}
}

func TestAdditionalServicePortsForNonOpenClawDesktopDoesNotExposeControlUI(t *testing.T) {
	for _, instanceType := range []string{"ubuntu", "webtop", "debian", "custom", "hermes"} {
		t.Run(instanceType, func(t *testing.T) {
			ports := additionalServicePortsForInstance(instanceType, DefaultDesktopTargetPort)
			if len(ports) != 0 {
				t.Fatalf("additionalServicePortsForInstance(%q, 3001) = %#v, want no control-ui port", instanceType, ports)
			}
		})
	}
}

func TestAdditionalServicePortsKeepsExistingDesktopWebsocketPair(t *testing.T) {
	ports := additionalServicePortsForInstance("custom", 3000)
	if len(ports) != 2 || ports[0] != 3000 || ports[1] != 8082 {
		t.Fatalf("additionalServicePortsForInstance(custom, 3000) = %#v, want []int32{3000, 8082}", ports)
	}
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

func TestBuildGatewayEnvProvidesOpenClawGatewayTokenFromServerSideInstanceToken(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example.test/api/v1/gateway/llm")
	token := "test-instance-gateway-token"
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{
			active: []models.LLMModel{{
				ID:        1,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}},
		},
	}

	env, err := service.buildGatewayEnv(&models.Instance{
		ID:          42,
		Type:        "openclaw",
		AccessToken: &token,
	})
	if err != nil {
		t.Fatalf("buildGatewayEnv() error = %v", err)
	}

	if env["OPENCLAW_GATEWAY_TOKEN"] != token {
		t.Fatalf("OPENCLAW_GATEWAY_TOKEN was not populated from the server-side instance token")
	}
	if env["CLAWMANAGER_INSTANCE_TOKEN"] != token {
		t.Fatalf("CLAWMANAGER_INSTANCE_TOKEN was not preserved")
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

func TestBuildGatewayEnvDoesNotInjectOpenClawGatewayTokenForNonOpenClaw(t *testing.T) {
	token := "test-instance-gateway-token"
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{err: errors.New("should not be called")},
	}

	env, err := service.buildGatewayEnv(&models.Instance{
		ID:          42,
		Type:        "ubuntu",
		AccessToken: &token,
	})
	if err != nil {
		t.Fatalf("buildGatewayEnv() error = %v", err)
	}
	if _, ok := env["OPENCLAW_GATEWAY_TOKEN"]; ok {
		t.Fatalf("OPENCLAW_GATEWAY_TOKEN was injected for non-OpenClaw instance")
	}
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
