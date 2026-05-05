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

func (r stubLLMModelRepository) List() ([]models.LLMModel, error) {
	return nil, nil
}

func (r stubLLMModelRepository) ListActive() ([]models.LLMModel, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.active, nil
}

func (r stubLLMModelRepository) GetByID(id int) (*models.LLMModel, error) {
	return nil, nil
}

func (r stubLLMModelRepository) GetByDisplayName(displayName string) (*models.LLMModel, error) {
	return nil, nil
}

func (r stubLLMModelRepository) Save(model *models.LLMModel) error {
	return nil
}

func (r stubLLMModelRepository) Delete(id int) error {
	return nil
}

func TestAdditionalServicePortsForOpenClawFreshInstanceIncludesControlUI(t *testing.T) {
	ports := additionalServicePortsForInstance("openclaw", DefaultDesktopTargetPort)
	if len(ports) != 1 || ports[0] != DefaultControlUITargetPort {
		t.Fatalf("additionalServicePortsForInstance(openclaw, 3001) = %#v, want []int32{18789}", ports)
	}
}

func TestAdditionalServicePortsForNonOpenClawDesktopDoesNotExposeControlUI(t *testing.T) {
	for _, instanceType := range []string{"ubuntu", "webtop", "debian", "custom"} {
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

func TestBuildGatewayEnvProvidesOpenClawGatewayTokenFromServerSideInstanceToken(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example.test/api/v1/gateway/llm")
	token := "test-instance-gateway-token"
	service := &instanceService{
		llmModelRepo: stubLLMModelRepository{
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

func TestBuildGatewayEnvDoesNotInjectOpenClawGatewayTokenForNonOpenClaw(t *testing.T) {
	token := "test-instance-gateway-token"
	service := &instanceService{
		llmModelRepo: stubLLMModelRepository{err: errors.New("should not be called")},
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
