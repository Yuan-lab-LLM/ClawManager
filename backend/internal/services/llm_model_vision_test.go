package services

import (
	"clawreef/internal/models"
	"testing"
)

func TestSaveVisionOverrideCompatibility(t *testing.T) {
	for _, value := range []bool{true, false} {
		repo := &reasoningLLMModelRepository{byID: &models.LLMModel{ID: 10, VisionSupportOverride: &value, SupportsVision: true, CapabilitySource: "provider_metadata"}}
		svc := NewLLMModelService(repo)
		req := validReasoningModelRequest()
		req.ID = 10
		got, err := svc.SaveModel(req)
		if err != nil || got.VisionSupportOverride == nil || *got.VisionSupportOverride != value {
			t.Fatalf("legacy client erased override: %v %+v", err, got)
		}
		req.SupportsVision, req.CapabilitySource = true, "provider_metadata"
		got, err = svc.SaveModel(req)
		if err != nil || *got.VisionSupportOverride != value {
			t.Fatal("discovery erased override", err)
		}
		opposite := !value
		req.VisionSupportOverride = &opposite
		got, err = svc.SaveModel(req)
		if err != nil || got.VisionSupportOverride == nil || *got.VisionSupportOverride != opposite {
			t.Fatal("explicit update failed", err)
		}
		req.ResetVisionSupportOverride = true
		got, err = svc.SaveModel(req)
		if err != nil || got.VisionSupportOverride != nil || !got.SupportsVision {
			t.Fatal("reset must preserve discovered metadata", err)
		}
	}
}
