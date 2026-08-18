package services

import (
	"strings"
	"testing"

	"clawreef/internal/models"
)

type reasoningLLMModelRepository struct {
	byID  *models.LLMModel
	saved *models.LLMModel
}

func (r *reasoningLLMModelRepository) List() ([]models.LLMModel, error)       { return nil, nil }
func (r *reasoningLLMModelRepository) ListActive() ([]models.LLMModel, error) { return nil, nil }
func (r *reasoningLLMModelRepository) GetByID(int) (*models.LLMModel, error)  { return r.byID, nil }
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
		IsActive:          true,
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
