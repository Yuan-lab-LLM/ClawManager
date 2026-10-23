package services

import (
	"clawreef/internal/models"
	"encoding/json"
	"testing"
)

func TestGatewayVisionInjectionUsesOwnerScopeAndExplicitChoices(t *testing.T) {
	yes, no := true, false
	s := &instanceService{llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{
		{ID: 1, DisplayName: "vision-private", ProviderType: models.ProviderTypeLocal, IsActive: true, VisionSupportOverride: &yes, VisibilityScope: "selected_users", AllowedUserIDs: []int{1}},
		{ID: 2, DisplayName: "disabled", IsActive: true, SupportsVision: true, CapabilitySource: "provider_metadata", VisionSupportOverride: &no},
		{ID: 3, DisplayName: "unknown", IsActive: true},
		{ID: 4, DisplayName: "unsupported-transport", ProviderType: models.ProviderTypeGoogle, IsActive: true, VisionSupportOverride: &yes},
		{ID: 5, DisplayName: "image-only", ModelKind: models.ModelKindImage, ProviderType: models.ProviderTypeLocal, IsActive: true, VisionSupportOverride: &yes, ImageGenerationEnabled: true},
	}}}
	for _, owner := range []int{1, 2} {
		injection, err := s.resolveGatewayModelInjection(owner)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]bool
		if err := json.Unmarshal([]byte(injection.visionJSON), &got); err != nil {
			t.Fatal(err)
		}
		if got["auto"] != (owner == 1) || got["disabled"] || got["unknown"] || got["unsupported-transport"] {
			t.Fatalf("owner %d: %#v", owner, got)
		}
		_, visible := got["vision-private"]
		if visible != (owner == 1) {
			t.Fatal("hidden model leaked into capability map")
		}
		if _, exists := got["image-only"]; exists {
			t.Fatal("image model advertised as vision chat")
		}
		var names []string
		if err := json.Unmarshal([]byte(injection.modelsJSON), &names); err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if name == "image-only" {
				t.Fatal("image model entered runtime chat list")
			}
		}
	}
}

func TestVisionCapabilityEnvironmentCannotBeOverridden(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GOVERNANCE_STRICT", "true")
	for _, runtime := range []string{"openclaw", "opencode", "hermes"} {
		if err := validateManagedRuntimeEnvironmentOverrides(runtime, map[string]string{"CLAWMANAGER_LLM_VISION": `{"auto":true}`}); err == nil {
			t.Fatalf("%s allowed vision override", runtime)
		}
	}
}
