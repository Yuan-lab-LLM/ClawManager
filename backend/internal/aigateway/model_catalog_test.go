package aigateway

import (
	"testing"

	"clawreef/internal/models"
)

type catalogModelRepository struct {
	active []models.LLMModel
}

func (r *catalogModelRepository) List() ([]models.LLMModel, error) { return r.active, nil }
func (r *catalogModelRepository) ListActive() ([]models.LLMModel, error) {
	return r.active, nil
}
func (r *catalogModelRepository) GetByID(int) (*models.LLMModel, error) { return nil, nil }
func (r *catalogModelRepository) GetByDisplayName(string) (*models.LLMModel, error) {
	return nil, nil
}
func (r *catalogModelRepository) Save(*models.LLMModel) error { return nil }
func (r *catalogModelRepository) Delete(int) error            { return nil }

func TestAvailableAndRequestedModelsRequireExplicitDeployment(t *testing.T) {
	configured := models.LLMModel{
		ID:                2,
		DisplayName:       "icompify",
		ProviderType:      models.ProviderTypeOpenAICompatible,
		ProtocolType:      models.ProtocolTypeOpenAICompatible,
		ProviderModelName: "qwen3.8",
		IsActive:          true,
	}
	if err := models.SetLLMProviderModels(&configured, []models.LLMProviderModel{
		{ID: "qwen3.8"},
		{ID: "glm-5.2"},
		{ID: "deepseek-v4-pro"},
	}); err != nil {
		t.Fatal(err)
	}
	service := &service{modelRepo: &catalogModelRepository{active: []models.LLMModel{configured}}}

	available, err := service.ListAvailableModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 2 || available[0].DisplayName != "Auto" || available[1].DisplayName != "icompify" { t.Fatalf("unapproved model exposed: %#v", available) }
	if selected, err := service.resolveRequestedModel("glm-5.2"); err == nil || selected != nil { t.Fatalf("discovery must not authorize a model: %#v, %v", selected, err) }
	selected, err := service.resolveRequestedModel("icompify")
	if err != nil || selected == nil || selected.ID != 2 || selected.ProviderModelName != "qwen3.8" { t.Fatalf("configured alias unavailable: %#v, %v", selected, err) }
}
