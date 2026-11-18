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
func (r *catalogModelRepository) GetByDisplayName(name string) (*models.LLMModel, error) {
	for _, model := range r.active {
		if model.DisplayName == name {
			return &model, nil
		}
	}
	return nil, nil
}
func (r *catalogModelRepository) Save(*models.LLMModel) error { return nil }
func (r *catalogModelRepository) Delete(int) error            { return nil }

func TestAvailableAliasesAndRequestsUseSavedProviderCatalog(t *testing.T) {
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
	// A discovered-only entry is not persisted and must remain unauthorized,
	// even when both the hydrated field and discovery cache contain it.
	const discoveredOnly = "unsaved-discovery-model"
	configured.ProviderModels = append(configured.ProviderModels, models.LLMProviderModel{ID: discoveredOnly})
	service := &service{
		modelRepo: &catalogModelRepository{active: []models.LLMModel{configured}},
		expandedModelCatalog: &availableExpandedModelCatalogStub{active: []models.LLMModel{
			{ID: configured.ID, DisplayName: discoveredOnly, ProviderModelName: discoveredOnly, IsActive: true},
		}},
	}

	available, err := service.ListAvailableModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 2 || available[0].DisplayName != "Auto" || available[1].DisplayName != "icompify" {
		t.Fatalf("available aliases changed: %#v", available)
	}
	selected, err := service.resolveRequestedModel("glm-5.2")
	if err != nil || selected == nil || selected.ID != configured.ID || selected.ProviderModelName != "glm-5.2" || selected.CatalogProviderName != "icompify" {
		t.Fatalf("saved catalog member lost its provider identity: %#v, %v", selected, err)
	}
	if selected, err := service.resolveRequestedModel(discoveredOnly); err == nil || selected != nil {
		t.Fatalf("unsaved discovery authorized a model: %#v, %v", selected, err)
	}
	selected, err = service.resolveRequestedModel("icompify")
	if err != nil || selected == nil || selected.ID != configured.ID || selected.ProviderModelName != "qwen3.8" {
		t.Fatalf("configured alias unavailable: %#v, %v", selected, err)
	}
}
