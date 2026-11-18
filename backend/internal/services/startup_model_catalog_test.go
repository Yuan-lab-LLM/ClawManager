package services

import (
	"clawreef/internal/models"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStartupModelCatalogRejectsMissingOwner(t *testing.T) {
	for _, instance := range []*models.Instance{nil, {UserID: 0}, {UserID: -1}} {
		s := &instanceService{llmModelRepo: &stubLLMModelRepository{}}
		raw, err := s.buildStartupModelCatalog(instance, "test-token")
		if !errors.Is(err, models.ErrModelUnavailable) || raw != "" {
			t.Fatalf("invalid owner became an authoritative empty catalog: %q %v", raw, err)
		}
	}
}

func TestStartupModelCatalogIsOwnerScopedAndContainsNoCredentials(t *testing.T) {
	s := &instanceService{llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{
		{ID: 1, DisplayName: "same name", RuntimeModelID: "cm-deploy-v1-new", ProviderModelName: "qwen", ContextWindowTokens: 256000, VisibilityScope: "all", IsActive: true},
		{ID: 2, DisplayName: "hidden", RuntimeModelID: "cm-deploy-v1-hidden", ProviderModelName: "hidden", VisibilityScope: "selected_users", AllowedUserIDs: []int{2}, IsActive: true},
	}}}
	raw, err := s.buildStartupModelCatalog(&models.Instance{UserID: 1, RuntimeGeneration: 7}, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Generation int                          `json:"generation"`
		Owner      int                          `json:"owner_user_id"`
		Complete   bool                         `json:"complete"`
		Models     []models.RuntimeCatalogModel `json:"models"`
	}
	if err = json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Complete || doc.Owner != 1 || doc.Generation != 7 || len(doc.Models) != 2 || doc.Models[1].StableID != "cm-deploy-v1-new" {
		t.Fatal("bad owner-scoped snapshot", raw)
	}
	if strings.Contains(raw, "hidden") || strings.Contains(raw, "secret-token") || strings.Contains(raw, "base_url") || strings.Contains(raw, "allowed_user_ids") {
		t.Fatal("snapshot leaked private metadata")
	}
}
