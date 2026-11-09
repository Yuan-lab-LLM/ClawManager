package services

import (
	"clawreef/internal/models"
	"testing"
)

func TestSaveModelQualityTier(t *testing.T) {
	for _, tier := range []int{-1, 0, 1, 2, 3, 4, 5} {
		repo := &reasoningLLMModelRepository{}
		req := validReasoningModelRequest()
		req.QualityTier = &tier
		got, err := NewLLMModelService(repo).SaveModel(req)
		if tier < 0 || tier > 4 {
			if err == nil || repo.saved != nil {
				t.Fatalf("accepted invalid tier %d", tier)
			}
		} else if err != nil || got.QualityTier != tier || repo.saved.QualityTier != tier {
			t.Fatalf("tier %d not persisted: %+v %v", tier, got, err)
		}
	}
}

func TestSaveModelPreservesOmittedQualityTier(t *testing.T) {
	repo := &reasoningLLMModelRepository{byID: &models.LLMModel{ID: 10, QualityTier: 3}}
	req := validReasoningModelRequest()
	req.ID = 10
	got, err := NewLLMModelService(repo).SaveModel(req)
	if err != nil || got.QualityTier != 3 {
		t.Fatalf("old client erased tier: %+v %v", got, err)
	}
	tier := 0
	req.QualityTier = &tier
	got, err = NewLLMModelService(repo).SaveModel(req)
	if err != nil || got.QualityTier != 0 {
		t.Fatalf("explicit reset failed: %+v %v", got, err)
	}
}
