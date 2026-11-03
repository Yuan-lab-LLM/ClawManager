package services

import (
	"clawreef/internal/models"
	"testing"
)

func TestSaveModelToolOverride(t *testing.T) {
	for _, value := range []bool{true, false} {
		repo := &reasoningLLMModelRepository{}
		req := validReasoningModelRequest()
		req.ToolsSupportOverride = &value
		got, err := NewLLMModelService(repo).SaveModel(req)
		if err != nil || got.ToolsSupportOverride == nil || *got.ToolsSupportOverride != value || repo.saved.ToolsSupportOverride == nil || *repo.saved.ToolsSupportOverride != value {
			t.Fatalf("tool override %v not persisted: %+v %v", value, got, err)
		}
	}
}

func TestSaveModelPreservesManualToolChoiceAndVision(t *testing.T) {
	for _, value := range []bool{true, false} {
		repo := &reasoningLLMModelRepository{byID: &models.LLMModel{ID: 10, ToolsSupportOverride: &value, SupportsVision: true, CapabilitySource: "provider_metadata"}}
		req := validReasoningModelRequest()
		req.ID = 10
		got, err := NewLLMModelService(repo).SaveModel(req)
		if err != nil || got.ToolsSupportOverride == nil || *got.ToolsSupportOverride != value || !got.SupportsVision || got.CapabilitySource != "provider_metadata" {
			t.Fatalf("old client erased manual choice or vision: %+v %v", got, err)
		}
		opposite := !value
		req.ToolsSupportOverride = &opposite
		got, err = NewLLMModelService(repo).SaveModel(req)
		if err != nil || got.ToolsSupportOverride == nil || *got.ToolsSupportOverride != opposite || !got.SupportsVision {
			t.Fatalf("explicit tool change failed or changed vision: %+v %v", got, err)
		}
	}
}

func TestSaveModelDiscoveryDoesNotEraseToolOverride(t *testing.T) {
	no := false
	repo := &reasoningLLMModelRepository{byID: &models.LLMModel{ID: 10, ToolsSupportOverride: &no}}
	req := validReasoningModelRequest()
	req.ID, req.SupportsTools, req.CapabilitySource = 10, true, "provider_metadata"
	got, err := NewLLMModelService(repo).SaveModel(req)
	if err != nil || got.ToolsSupportOverride == nil || *got.ToolsSupportOverride {
		t.Fatalf("discovery erased explicit opt-out: %+v %v", got, err)
	}
}
