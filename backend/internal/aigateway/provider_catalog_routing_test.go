package aigateway

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"errors"
	"testing"
)

func TestProviderCatalogSelectionRetainsRequestedModelAndOwnerScope(t *testing.T) {
	parent := routingModels()[0]
	parent.DisplayName = "icompify"
	parent.ModelGroup = "catalog"
	parent.ProviderModelName = "deepseek-flash"
	if err := models.SetLLMProviderModels(&parent, []models.LLMProviderModel{{ID: "deepseek-flash"}, {ID: "glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	unrelated := routingModels()[1]
	unrelated.ModelGroup = "catalog"
	unrelated.ProviderModelName = "other-model"
	private := parent
	private.ID = 3
	private.DisplayName = "private"
	private.VisibilityScope = "selected_users"
	private.AllowedUserIDs = []int{1}
	if err := models.SetLLMProviderModels(&private, []models.LLMProviderModel{{ID: "secret-model"}}); err != nil {
		t.Fatal(err)
	}
	s := routingGateway([]models.LLMModel{parent, unrelated, private}, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(s.modelRepo, 2)
	selected, err := s.resolveRequestedModel("glm-5.3")
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != parent.ID || selected.ProviderModelName != "glm-5.3" {
		t.Fatalf("wrong physical model or grant identity: %+v", selected)
	}
	candidates := s.deploymentCandidates(selected)
	if len(candidates) != 1 || candidates[0].ProviderModelName != "glm-5.3" || candidates[0].ID != parent.ID {
		t.Fatalf("named selection fell back to another model: %+v", candidates)
	}
	if _, err = s.resolveRequestedModel("secret-model"); err == nil {
		t.Fatal("hidden provider catalog became routable")
	}
	legacy, err := s.resolveRequestedModel("icompify")
	if err != nil || legacy.ProviderModelName != "deepseek-flash" {
		t.Fatal("provider's existing default alias changed")
	}
}

// The provider stays granted/active while an administrator removes one saved
// child model between resolution and the final deployment-candidate read.
type changingProviderCatalogRepository struct {
	*stubLLMModelRepository
	after    []models.LLMModel
	reads    int
	afterErr error
}

func (r *changingProviderCatalogRepository) ListActive() ([]models.LLMModel, error) {
	r.reads++
	if r.reads > 1 {
		return r.after, r.afterErr
	}
	return r.active, nil
}
func TestProviderCatalogRemovalBeforeDispatchIsDenied(t *testing.T) {
	parent := routingModels()[0]
	parent.DisplayName = "icompify"
	parent.ModelGroup = "catalog"
	parent.ProviderModelName = "deepseek-flash"
	if err := models.SetLLMProviderModels(&parent, []models.LLMProviderModel{{ID: "deepseek-flash"}, {ID: "glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	updated := parent
	if err := models.SetLLMProviderModels(&updated, []models.LLMProviderModel{{ID: "deepseek-flash"}}); err != nil {
		t.Fatal(err)
	}
	s := routingGateway(nil, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(&changingProviderCatalogRepository{stubLLMModelRepository: &stubLLMModelRepository{active: []models.LLMModel{parent}}, after: []models.LLMModel{updated}}, 2)
	req := routingRequest("hello")
	req.Model = "glm-5.3"
	if _, err := s.prepareChatRequest(t.Context(), 2, req); err == nil {
		t.Fatal("removed catalog model can fall back to a stale selection before dispatch")
	}
	selected := models.ExpandLLMModelCatalog([]models.LLMModel{parent})[1]
	s.modelRepo = repository.ForModelUser(&stubLLMModelRepository{active: []models.LLMModel{updated}}, 2)
	if candidates := s.deploymentCandidates(&selected); len(candidates) != 0 {
		t.Fatal("removed saved model produced a stale fallback candidate")
	}
}

func TestProviderCatalogLookupFailureBeforeDispatchIsDenied(t *testing.T) {
	parent := routingModels()[0]
	parent.DisplayName = "icompify"
	parent.ProviderModelName = "deepseek-flash"
	if err := models.SetLLMProviderModels(&parent, []models.LLMProviderModel{{ID: "glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	s := routingGateway(nil, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(&changingProviderCatalogRepository{stubLLMModelRepository: &stubLLMModelRepository{active: []models.LLMModel{parent}}, afterErr: errors.New("catalog unavailable")}, 2)
	req := routingRequest("hello")
	req.Model = "glm-5.3"
	if _, err := s.prepareChatRequest(t.Context(), 2, req); err == nil {
		t.Fatal("catalog lookup failure dispatched a stale saved model")
	}
}

func TestLegacyRealModelResolutionAndFailoverStayAuthoritative(t *testing.T) {
	items := routingModels()
	items[0].DisplayName, items[0].ProviderModelName = "flash (epai)", "org/flash"
	items[1].ModelGroup, items[1].ProviderModelName = items[0].ModelGroup, "other-model"
	s := routingGateway(items, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(s.modelRepo, 2)
	selected, err := s.resolveRequestedModel("org/flash")
	if err != nil || selected == nil || selected.ID != items[0].ID || selected.ProviderModelName != "org/flash" {
		t.Fatalf("legacy real model did not resolve: %+v %v", selected, err)
	}
	if got := s.deploymentCandidates(selected); len(got) != 1 || got[0].ID != selected.ID {
		t.Fatalf("named model changed upstream identity: %+v", got)
	}
	s.modelRepo = repository.ForModelUser(&stubLLMModelRepository{active: items[1:]}, 2)
	if got := s.deploymentCandidates(selected); len(got) != 0 {
		t.Fatalf("removed real model resurrected stale selection: %+v", got)
	}
}

func TestNamedDisplayAliasDoesNotFailOverToDifferentRealModel(t *testing.T) {
	items := routingModels()
	items[0].DisplayName, items[0].ProviderModelName = "flash", "flash"
	items[1].ModelGroup, items[1].ProviderModelName = items[0].ModelGroup, "other-model"
	s := routingGateway(items, &stubAuditEventService{})
	selected, err := s.resolveRequestedModel("flash")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.deploymentCandidates(selected); len(got) != 1 || got[0].ProviderModelName != "flash" {
		t.Fatalf("display alias changed upstream identity: %+v", got)
	}
}

func TestProviderDefaultAliasRetainsUngroupedSavedCatalog(t *testing.T) {
	parent := routingModels()[0]
	parent.DisplayName, parent.ModelGroup, parent.ProviderModelName = "icompify", "", "flash"
	if err := models.SetLLMProviderModels(&parent, []models.LLMProviderModel{{ID: "flash"}, {ID: "glm"}}); err != nil {
		t.Fatal(err)
	}
	s := routingGateway([]models.LLMModel{parent}, &stubAuditEventService{})
	for _, name := range []string{"icompify", "flash", "glm"} {
		selected, err := s.resolveRequestedModel(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.deploymentCandidates(selected); len(got) != 1 || got[0].ProviderModelName != selected.ProviderModelName {
			t.Fatalf("ungrouped saved catalog alias %q lost its deployment: %+v", name, got)
		}
	}
}

func TestCanonicalRealModelWinsOverCollidingDisplayAlias(t *testing.T) {
	items := routingModels()
	items[0].DisplayName, items[0].ProviderModelName = "model-b", "model-a"
	items[1].DisplayName, items[1].ProviderModelName = "Friendly", "model-b"
	s := routingGateway(items, &stubAuditEventService{})
	s.modelRepo = repository.ForModelUser(s.modelRepo, 2)
	selected, err := s.resolveRequestedModel("model-b")
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != items[1].ID || selected.ProviderModelName != "model-b" {
		t.Fatalf("display alias hijacked canonical upstream ID: %+v", selected)
	}
	if got := s.deploymentCandidates(selected); len(got) != 1 || got[0].ProviderModelName != "model-b" {
		t.Fatalf("canonical request changed upstream identity: %+v", got)
	}
}
