package services

import (
	"clawreef/internal/models"
	"errors"
	"strings"
	"testing"
)

func TestModelAccessSaveDefaultsAndLegacyPreservation(t *testing.T) {
	repo := &reasoningLLMModelRepository{}
	svc := NewLLMModelService(repo)
	req := validReasoningModelRequest()
	m, err := svc.SaveModel(req)
	if err != nil || m.VisibilityScope != "all" {
		t.Fatalf("default: %+v %v", m, err)
	}
	repo.byID = &models.LLMModel{ID: 7, VisibilityScope: "selected_users", AllowedUserIDs: []int{2}, AccessPolicyVersion: 3}
	req.ID = 7
	m, err = svc.SaveModel(req)
	if err != nil || m.VisibilityScope != "selected_users" || !m.VisibleTo(2) || m.VisibleTo(3) || m.UpdateAccessPolicy {
		t.Fatalf("legacy update lost access: %+v %v", m, err)
	}
	scope, version := "all", 2
	req.VisibilityScope, req.AccessPolicyVersion = &scope, &version
	if _, err := svc.SaveModel(req); !errors.Is(err, models.ErrModelAccessConflict) {
		t.Fatalf("stale save: %v", err)
	}
	version = 3
	m, err = svc.SaveModel(req)
	if err != nil || !m.UpdateAccessPolicy || len(m.AllowedUserIDs) != 0 {
		t.Fatalf("reset: %+v %v", m, err)
	}
}

func TestModelAccessRejectsEmptyInvalidAndDeduplicates(t *testing.T) {
	scope := "selected_users"
	for _, ids := range [][]int{{}, {0}, {-1}} {
		m := &models.LLMModel{}
		if err := applyModelAccessPolicy(m, nil, SaveLLMModelRequest{VisibilityScope: &scope, AllowedUserIDs: &ids}); !errors.Is(err, models.ErrInvalidModelAccess) {
			t.Fatalf("accepted %v: %v", ids, err)
		}
	}
	ids := []int{2, 1, 2}
	m := &models.LLMModel{}
	if err := applyModelAccessPolicy(m, nil, SaveLLMModelRequest{VisibilityScope: &scope, AllowedUserIDs: &ids}); err != nil || len(m.AllowedUserIDs) != 2 {
		t.Fatalf("dedup: %+v %v", m, err)
	}
}

func TestModelAccessUnchangedPolicyDoesNotReplaceGrants(t *testing.T) {
	current := &models.LLMModel{VisibilityScope: "selected_users", AllowedUserIDs: []int{2, 1}, AccessPolicyVersion: 4}
	scope, version, ids := "selected_users", 4, []int{1, 2}
	model := &models.LLMModel{}
	err := applyModelAccessPolicy(model, current, SaveLLMModelRequest{VisibilityScope: &scope, AccessPolicyVersion: &version, AllowedUserIDs: &ids})
	if err != nil || model.UpdateAccessPolicy {
		t.Fatalf("unchanged policy replaced: %+v %v", model, err)
	}
}

func TestModelAccessInjectionUsesOwnerAndVisibleContext(t *testing.T) {
	s := &instanceService{llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{
		{ID: 1, DisplayName: "private", ModelGroup: "general", ContextWindowTokens: 100, VisibilityScope: "selected_users", AllowedUserIDs: []int{1}},
		{ID: 2, DisplayName: "public", ModelGroup: "general", ContextWindowTokens: 32000, VisibilityScope: "all"},
	}}}
	injection, err := s.resolveGatewayModelInjection(2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(injection.modelsJSON, "private") || injection.defaultContextWindow != 32000 {
		t.Fatalf("invisible deployment leaked: %+v", injection)
	}
	if _, err := s.resolveGatewayModelInjection(0); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("invalid owner accepted: %v", err)
	}
}
