package models

import (
	"encoding/json"
	"strings"
)

// LLMProviderModel is one upstream model exposed by a configured provider.
type LLMProviderModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// PopulateLLMProviderModels hydrates the API-facing provider model list from
// its persisted JSON representation. Legacy rows fall back to their single
// provider_model_name so existing installations remain compatible.
func PopulateLLMProviderModels(model *LLMModel) {
	if model == nil {
		return
	}
	model.ProviderModels = storedLLMProviderModels(model)
	if len(model.ProviderModels) > 0 {
		return
	}
	if providerModelName := strings.TrimSpace(model.ProviderModelName); providerModelName != "" {
		model.ProviderModels = []LLMProviderModel{{
			ID:          providerModelName,
			DisplayName: providerModelName,
		}}
	}
}

// SetLLMProviderModels normalizes and persists the complete upstream model
// list while guaranteeing that the configured default model remains usable.
func SetLLMProviderModels(model *LLMModel, providerModels []LLMProviderModel) error {
	if model == nil {
		return nil
	}
	normalized := normalizeLLMProviderModels(providerModels)
	defaultModel := strings.TrimSpace(model.ProviderModelName)
	if defaultModel != "" && !containsLLMProviderModel(normalized, defaultModel) {
		normalized = append([]LLMProviderModel{{ID: defaultModel, DisplayName: defaultModel}}, normalized...)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	encoded := string(raw)
	model.ProviderModelsJSON = &encoded
	model.ProviderModels = normalized
	return nil
}

// ExpandLLMModelCatalog converts provider configuration rows into the actual
// model aliases that runtime instances and the gateway expose. A legacy row
// without a persisted provider list keeps its historical display name.
func ExpandLLMModelCatalog(configured []LLMModel) []LLMModel {
	if len(configured) == 0 {
		return []LLMModel{}
	}

	modelIDCounts := make(map[string]int)
	storedByIndex := make([][]LLMProviderModel, len(configured))
	for index := range configured {
		stored := storedLLMProviderModels(&configured[index])
		storedByIndex[index] = stored
		for _, providerModel := range stored {
			modelIDCounts[strings.ToLower(providerModel.ID)]++
		}
	}

	expanded := make([]LLMModel, 0, len(configured))
	for index, configuredModel := range configured {
		providerModels := storedByIndex[index]
		if len(providerModels) == 0 {
			PopulateLLMReasoningCapability(&configuredModel)
			expanded = append(expanded, configuredModel)
			continue
		}

		for _, providerModel := range providerModels {
			item := configuredModel
			item.ProviderModelName = providerModel.ID
			item.DisplayName = providerModel.ID
			if modelIDCounts[strings.ToLower(providerModel.ID)] > 1 {
				item.DisplayName = strings.TrimSpace(configuredModel.DisplayName) + "/" + providerModel.ID
			}
			item.ProviderModels = nil
			item.ProviderModelsJSON = nil
			PopulateLLMReasoningCapability(&item)
			if !item.SupportsReasoning {
				item.ReasoningEnabled = false
			}
			expanded = append(expanded, item)
		}
	}
	return expanded
}

func storedLLMProviderModels(model *LLMModel) []LLMProviderModel {
	if model == nil || model.ProviderModelsJSON == nil || strings.TrimSpace(*model.ProviderModelsJSON) == "" {
		return nil
	}
	var providerModels []LLMProviderModel
	if err := json.Unmarshal([]byte(*model.ProviderModelsJSON), &providerModels); err != nil {
		return nil
	}
	return normalizeLLMProviderModels(providerModels)
}

func normalizeLLMProviderModels(providerModels []LLMProviderModel) []LLMProviderModel {
	normalized := make([]LLMProviderModel, 0, len(providerModels))
	seen := make(map[string]struct{}, len(providerModels))
	for _, providerModel := range providerModels {
		id := strings.TrimSpace(providerModel.ID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		displayName := strings.TrimSpace(providerModel.DisplayName)
		if displayName == "" {
			displayName = id
		}
		normalized = append(normalized, LLMProviderModel{ID: id, DisplayName: displayName})
	}
	return normalized
}

func containsLLMProviderModel(providerModels []LLMProviderModel, id string) bool {
	for _, providerModel := range providerModels {
		if strings.EqualFold(providerModel.ID, id) {
			return true
		}
	}
	return false
}
