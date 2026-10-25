package models

import "testing"

func TestExpandLLMModelCatalogExpandsPersistedProviderModels(t *testing.T) {
	configured := LLMModel{
		ID:                2,
		DisplayName:       "icompify",
		ProviderModelName: "qwen3.8",
		IsActive:          true,
	}
	if err := SetLLMProviderModels(&configured, []LLMProviderModel{
		{ID: "qwen3.8"},
		{ID: "glm-5.2"},
		{ID: "qwen3.8", DisplayName: "duplicate"},
	}); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandLLMModelCatalog([]LLMModel{configured})
	if len(expanded) != 2 {
		t.Fatalf("expanded model count = %d, want 2", len(expanded))
	}
	if expanded[0].DisplayName != "qwen3.8" || expanded[0].ProviderModelName != "qwen3.8" {
		t.Fatalf("unexpected first expanded model: %#v", expanded[0])
	}
	if expanded[1].DisplayName != "glm-5.2" || expanded[1].ProviderModelName != "glm-5.2" {
		t.Fatalf("unexpected second expanded model: %#v", expanded[1])
	}
}

func TestExpandLLMModelCatalogPreservesLegacyDisplayName(t *testing.T) {
	expanded := ExpandLLMModelCatalog([]LLMModel{{
		DisplayName:       "Internal Secure Model",
		ProviderModelName: "provider-model-v1",
	}})
	if len(expanded) != 1 || expanded[0].DisplayName != "Internal Secure Model" {
		t.Fatalf("legacy model was not preserved: %#v", expanded)
	}
}

func TestExpandLLMModelCatalogQualifiesDuplicateProviderModelIDs(t *testing.T) {
	first := LLMModel{DisplayName: "provider-a", ProviderModelName: "shared"}
	second := LLMModel{DisplayName: "provider-b", ProviderModelName: "shared"}
	if err := SetLLMProviderModels(&first, []LLMProviderModel{{ID: "shared"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetLLMProviderModels(&second, []LLMProviderModel{{ID: "shared"}}); err != nil {
		t.Fatal(err)
	}

	expanded := ExpandLLMModelCatalog([]LLMModel{first, second})
	if len(expanded) != 2 || expanded[0].DisplayName != "provider-a/shared" || expanded[1].DisplayName != "provider-b/shared" {
		t.Fatalf("duplicate provider model aliases were not qualified: %#v", expanded)
	}
}
