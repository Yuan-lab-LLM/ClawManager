package services

import (
	"clawreef/internal/models"
	"math"
	"testing"
)

func TestImageBudgetEstimatePreservesClearAndExplicitZero(t *testing.T) {
	price := 0.2
	old := &models.LLMModel{ModelKind: "image", ImagePricePerImage: &price}
	model := &models.LLMModel{}
	if err := applyModelOperations(model, old, SaveLLMModelRequest{}); err != nil || model.ImagePricePerImage == nil || *model.ImagePricePerImage != 0.2 {
		t.Fatal(model, err)
	}
	zero := 0.0
	if err := applyModelOperations(model, old, SaveLLMModelRequest{ImagePricePerImage: &zero}); err != nil || model.ImagePricePerImage == nil || *model.ImagePricePerImage != 0 {
		t.Fatal(model, err)
	}
	if err := applyModelOperations(model, old, SaveLLMModelRequest{ResetImagePrice: true}); err != nil || model.ImagePricePerImage != nil {
		t.Fatal(model, err)
	}
	for _, bad := range []float64{-1, 1000001, 1e-10, math.NaN(), math.Inf(1)} {
		if err := applyModelOperations(model, nil, SaveLLMModelRequest{ImagePricePerImage: &bad}); err == nil {
			t.Fatal("invalid price accepted", bad)
		}
	}
	if err := applyModelOperations(model, nil, SaveLLMModelRequest{ImagePricePerImage: &zero, ResetImagePrice: true}); err == nil {
		t.Fatal("ambiguous price accepted")
	}
}

func TestSaveModelOperationCompatibility(t *testing.T) {
	repo := &reasoningLLMModelRepository{byID: &models.LLMModel{ID: 10, ModelKind: "image", ImageGenerationEnabled: true, ImageEditEnabled: true}}
	req := validReasoningModelRequest()
	req.ID = 10
	req.ProviderType, req.ProtocolType = models.ProviderTypeLocal, models.ProtocolTypeOpenAICompatible
	req.ReasoningEnabled = nil
	svc := NewLLMModelService(repo)
	got, err := svc.SaveModel(req)
	if err != nil || got.ModelKind != "image" || !got.ImageGenerationEnabled || !got.ImageEditEnabled {
		t.Fatalf("legacy update lost choices: %+v %v", got, err)
	}
	no := false
	req.ImageEditEnabled = &no
	got, err = svc.SaveModel(req)
	if err != nil || !got.ImageGenerationEnabled || got.ImageEditEnabled {
		t.Fatalf("explicit false not saved: %+v %v", got, err)
	}

	chat := "chat"
	req.ModelKind = &chat
	if _, err := svc.SaveModel(req); err == nil {
		t.Fatal("image flags allowed on chat model")
	}
	req.ImageGenerationEnabled = &no
	got, err = svc.SaveModel(req)
	if err != nil || got.ModelKind != "chat" {
		t.Fatalf("explicit type change failed: %+v %v", got, err)
	}
}

func TestSaveModelOperationsDefaultAndValidation(t *testing.T) {
	for _, tc := range []struct {
		kind, protocol          string
		generation, edit, valid bool
	}{
		{"chat", "openai-compatible", false, false, true},
		{"image", "openai-compatible", true, false, true},
		{"image", "openai", false, true, true},
		{"image", "anthropic", true, false, false},
		{"image", "anthropic", false, true, false},
		{"image", "google", true, false, false},
		{"chat", "openai", true, false, false},
		{"other", "openai", false, false, true},
		{"invalid", "openai", false, false, false},
		{"", "openai", false, false, false},
	} {
		m := &models.LLMModel{ProtocolType: tc.protocol, ProviderType: models.ProviderTypeLocal}
		err := applyModelOperations(m, nil, SaveLLMModelRequest{ModelKind: &tc.kind, ImageGenerationEnabled: &tc.generation, ImageEditEnabled: &tc.edit})
		if (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	m := &models.LLMModel{}
	if err := applyModelOperations(m, nil, SaveLLMModelRequest{}); err != nil || m.ModelKind != "chat" || m.ImageGenerationEnabled || m.ImageEditEnabled {
		t.Fatal("new legacy defaults", m, err)
	}
}

func TestDiscoveredImageModelsDoNotSuggestChat(t *testing.T) {
	for _, name := range []string{"gpt-image-1", "vendor/gpt-image-2", "dall-e-3", "flux.1-dev", "stable-diffusion-xl"} {
		m := enrichDiscoveredModel(classifyDiscoveredModel(name, name), 0, "", []string{"reasoning", "image_input"})
		if m.ChatCapable || m.ModelKind != "image" || m.SuggestedQualityTier != 0 || m.SuggestedPool != "specialized" {
			t.Fatalf("%s: %+v", name, m)
		}
	}
	m := enrichDiscoveredModel(classifyDiscoveredModel("opaque", "Opaque"), 0, "image_generation", nil)
	if m.ChatCapable || m.ModelKind != "image" || m.ClassificationSource != "provider_metadata" {
		t.Fatal(m)
	}
	m = classifyDiscoveredModel("gpt-4o", "GPT-4o")
	if !m.ChatCapable || m.ModelKind == "image" {
		t.Fatal("vision chat model misclassified", m)
	}
}
