package services

import (
	"clawreef/internal/models"
	"testing"
)

func TestSaveReasoningControlModeLifecycle(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	svc := NewLLMModelService(repo)
	req := validReasoningModelRequest()
	req.BaseURL = "https://proxy.example/v1"
	mode := models.ReasoningControlEnableThinking
	req.ReasoningControlMode = &mode
	req.ReasoningEnabled = boolPointer(true)
	saved, err := svc.SaveModel(req)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.SupportsReasoning || !saved.ReasoningEnabled || saved.ReasoningControlMode != mode {
		t.Fatal("mode not persisted")
	}
	saved.ID = 8
	repo.byID = saved
	req.ID = 8
	req.ReasoningControlMode = nil
	req.ReasoningEnabled = nil
	saved, err = svc.SaveModel(req)
	if err != nil || saved.ReasoningControlMode != mode || !saved.ReasoningEnabled {
		t.Fatalf("old client lost mode: %+v %v", saved, err)
	}
	reset := ""
	req.ReasoningControlMode = &reset
	saved, err = svc.SaveModel(req)
	if err != nil || saved.ReasoningControlMode != "" || saved.SupportsReasoning || saved.ReasoningEnabled {
		t.Fatalf("reset failed: %+v %v", saved, err)
	}
}

func TestSaveReasoningControlModeRejectsInvalidFormat(t *testing.T) {
	for _, mode := range []string{"bad-format", models.ReasoningControlEnableThinking} {
		repo := &reasoningLLMModelRepository{}
		req := validReasoningModelRequest()
		req.ReasoningControlMode = &mode
		if mode != "bad-format" {
			req.ProviderType = models.ProviderTypeAnthropic
			req.ProtocolType = models.ProtocolTypeAnthropic
		}
		if _, err := NewLLMModelService(repo).SaveModel(req); err == nil {
			t.Fatal("expected validation error")
		}
		if repo.saved != nil {
			t.Fatal("invalid config persisted")
		}
	}
}
