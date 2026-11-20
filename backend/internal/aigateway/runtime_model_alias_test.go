package aigateway

import (
	"errors"
	"slices"
	"sort"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestRuntimeModelAliasesRespectOwnerAndDeployment(t *testing.T) {
	items := routingModels()
	items[0].DisplayName = "deepseek-v4.1-flash-public (openai)"
	items[1].DisplayName = "deepseek-v4.1-flash-public (other URL)"
	items[0].ProviderModelName, items[1].ProviderModelName = "same-upstream", "same-upstream"
	items[0].VisibilityScope, items[0].AllowedUserIDs = "selected_users", []int{1}
	for _, userID := range []int{1, 2} {
		s := routingGateway(items, &stubAuditEventService{})
		s.modelRepo = repository.ForModelUser(s.modelRepo, userID)
		for i, item := range items[:2] {
			selected, err := s.resolveRequestedModel(models.RuntimeModelAlias(item.DisplayName))
			if i == 0 && userID == 2 {
				if !errors.Is(err, models.ErrModelUnavailable) {
					t.Fatalf("hidden alias bypass: %v", err)
				}
				continue
			}
			if err != nil || selected.ID != item.ID {
				t.Fatalf("wrong deployment: %#v %v", selected, err)
			}
		}
		if _, err := s.resolveRequestedModel(models.RuntimeModelAlias("deleted model")); !errors.Is(err, models.ErrModelUnavailable) {
			t.Fatalf("unknown alias must fail closed: %v", err)
		}
	}
}

func TestRuntimeModelAliasRequestStillChecksKeyGroups(t *testing.T) {
	items := routingModels()
	items[0].DisplayName = "glm-5.3-public (openai)"
	s := routingGateway(items, &stubAuditEventService{})
	r := routingRequest("hello")
	r.Model = models.RuntimeModelAlias(items[0].DisplayName)
	r.AllowedModelGroups = []string{"not-authorized"}
	if _, err := s.prepareChatRequest(t.Context(), 1, r); err == nil {
		t.Fatal("runtime alias bypassed key model-group restriction")
	}
	r.AllowedModelGroups = nil
	p, err := s.prepareChatRequest(t.Context(), 1, r)
	if err != nil {
		t.Fatal(err)
	}
	if p.resolvedModel.ID != items[0].ID {
		t.Fatal("alias resolved to another deployment")
	}
}

type historicalAliasRepo struct {
	repository.LLMModelRepository
	aliases         map[string]int
	aliasReadErr    error
	providerReadErr error
}

func (r *historicalAliasRepo) RuntimeProviderRefs(ids []int) (map[int][]models.RuntimeProviderRef, error) {
	if r.providerReadErr != nil {
		return nil, r.providerReadErr
	}
	result := map[int][]models.RuntimeProviderRef{}
	for name, id := range r.aliases {
		if slices.Contains(ids, id) {
			result[id] = append(result[id], models.RuntimeProviderRef{ProviderID: name, ModelID: name})
		}
	}
	return result, nil
}

func (r *historicalAliasRepo) RuntimeModelAliases(ids []int) (map[int][]string, error) {
	if r.aliasReadErr != nil {
		return nil, r.aliasReadErr
	}
	result := map[int][]string{}
	for alias, id := range r.aliases {
		if slices.Contains(ids, id) {
			result[id] = append(result[id], alias)
		}
	}
	for id := range result {
		sort.Strings(result[id])
	}
	return result, nil
}

func TestRuntimeCatalogAliasesAreScopedAndDoNotCreateExtraModels(t *testing.T) {
	items := routingModels()
	items[0].VisibilityScope, items[0].AllowedUserIDs = "selected_users", []int{1}
	s := routingGateway(items, &stubAuditEventService{})
	r := &historicalAliasRepo{LLMModelRepository: s.modelRepo, aliases: map[string]int{
		"old private": items[0].ID, "old public": items[1].ID, "deleted": 999999,
	}}
	s.modelRepo = r
	for _, owner := range []int{1, 2} {
		catalog, err := s.RuntimeCatalog(owner, nil)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, entry := range catalog {
			if entry.ID == "old private" || entry.ID == "old public" {
				t.Fatal("alias listed as a new deployment")
			}
			for _, alias := range entry.Aliases {
				seen[alias] = true
			}
			if entry.ID != "auto" && len(entry.ProviderRefs) != len(entry.Aliases) {
				t.Fatal("provider refs missing")
			}
			for _, ref := range entry.ProviderRefs {
				if ref.ProviderID == "deleted" || (owner == 2 && ref.ProviderID == "old private") {
					t.Fatal("provider reference access leak")
				}
			}
		}
		if seen["deleted"] || seen["old private"] != (owner == 1) || !seen["old public"] {
			t.Fatalf("alias access leak or omission: %#v", seen)
		}
	}
	catalog, err := s.RuntimeCatalog(1, []string{"forbidden"})
	if err != nil || len(catalog) != 0 {
		t.Fatalf("key scope bypass: %v %v", catalog, err)
	}
	scoped := repository.ForModelUser(r, 2).(repository.RuntimeModelAliasCatalog)
	aliases, err := scoped.RuntimeModelAliases([]int{items[0].ID, items[1].ID, 999999})
	if err != nil || len(aliases[items[0].ID]) != 0 || len(aliases[999999]) != 0 || !slices.Contains(aliases[items[1].ID], "old public") {
		t.Fatalf("scoped alias reader leaked a hidden or deleted deployment: %v %v", aliases, err)
	}
	r.aliasReadErr = errors.New("database unavailable")
	if catalog, err := s.RuntimeCatalog(1, nil); err == nil || catalog != nil {
		t.Fatal("failed alias lookup was mistaken for a valid catalog")
	}
	r.aliasReadErr = nil
	refs, err := repository.ForModelUser(r, 2).(repository.RuntimeProviderRefCatalog).RuntimeProviderRefs([]int{items[0].ID, items[1].ID, 999999})
	if err != nil || len(refs[items[0].ID]) != 0 || len(refs[999999]) != 0 || len(refs[items[1].ID]) != 1 {
		t.Fatalf("scoped provider refs leaked: %v %v", refs, err)
	}
	r.providerReadErr = errors.New("provider registry unavailable")
	if catalog, err := s.RuntimeCatalog(1, nil); err == nil || catalog != nil {
		t.Fatal("provider registry failure returned a successful catalog")
	}
}

func TestRuntimeCatalogAddsStableIdentityWithoutChangingLegacyID(t *testing.T) {
	items := routingModels()
	items[0].RuntimeModelID = "cm-deploy-v1-01234567890123456789012345678901"
	s := routingGateway(items, &stubAuditEventService{})
	catalog, err := s.RuntimeCatalog(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog {
		if entry.ID == items[0].DisplayName {
			if entry.StableID != items[0].RuntimeModelID {
				t.Fatal("stable metadata missing")
			}
			return
		}
	}
	t.Fatal("legacy model ID changed before runtime negotiation")
}

func TestDeletedNameCannotBypassTombstoneThroughSameUpstreamName(t *testing.T) {
	items := routingModels()
	items[0].DisplayName = "recreated"
	items[0].ProviderModelName = "recreated"
	items[0].RuntimeModelID = "cm-deploy-v1-new"
	s := routingGateway(items, &stubAuditEventService{})
	s.modelRepo = &historicalAliasRepo{LLMModelRepository: s.modelRepo, aliases: map[string]int{"recreated": 999999, items[0].RuntimeModelID: items[0].ID}}
	if _, err := s.resolveRequestedModel("recreated"); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("old reference redirected", err)
	}
	current, err := s.resolveRequestedModel(items[0].RuntimeModelID)
	if err != nil || current.ID != items[0].ID {
		t.Fatal("new identity not callable", err)
	}
	catalog, err := s.RuntimeCatalog(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog {
		if item.StableID == items[0].RuntimeModelID {
			if !item.LegacyNameUnavailable {
				t.Fatal("unsafe display alias advertised")
			}
			return
		}
	}
	t.Fatal("new deployment missing")
}

func (r *historicalAliasRepo) ResolveRuntimeModel(ref string) (*models.LLMModel, error) {
	id, exists := r.aliases[ref]
	if !exists {
		return nil, nil
	}
	m, err := r.GetByID(id)
	if err != nil {
		return nil, err
	}
	if m == nil || !m.IsActive {
		return nil, models.ErrModelUnavailable
	}
	return m, nil
}

func TestHistoricalRuntimeModelAliasRemainsScoped(t *testing.T) {
	items := routingModels()
	items[0].DisplayName = "renamed model"
	items[0].VisibilityScope, items[0].AllowedUserIDs = "selected_users", []int{1}
	for _, owner := range []int{1, 2} {
		s := routingGateway(items, &stubAuditEventService{})
		s.modelRepo = repository.ForModelUser(&historicalAliasRepo{LLMModelRepository: s.modelRepo, aliases: map[string]int{"old model": items[0].ID, "stable-id": items[0].ID, "old provider/old model": items[0].ID, "provider/org/model": items[0].ID, "deleted model": 999999, "deleted provider/deleted model": 999999}}, owner)
		for _, ref := range []string{"old model", "stable-id", "old provider/old model", "provider/org/model"} {
			m, err := s.resolveRequestedModel(ref)
			if owner == 1 {
				if err != nil || m.ID != items[0].ID {
					t.Fatalf("lost historical identity: %v %v", m, err)
				}
			} else if !errors.Is(err, models.ErrModelUnavailable) {
				t.Fatalf("alias bypassed owner: %v", err)
			}
		}
		if _, err := s.resolveRequestedModel("deleted model"); !errors.Is(err, models.ErrModelUnavailable) {
			t.Fatalf("deleted alias fell through: %v", err)
		}
		if _, err := s.resolveRequestedModel("deleted provider/deleted model"); !errors.Is(err, models.ErrModelUnavailable) {
			t.Fatalf("deleted provider reference fell through: %v", err)
		}
		if owner == 1 {
			r := routingRequest("hello")
			r.Model, r.AllowedModelGroups = "old model", []string{"forbidden"}
			if _, err := s.prepareChatRequest(t.Context(), owner, r); err == nil {
				t.Fatal("historical alias bypassed key groups")
			}
			r.Model = "old provider/old model"
			if _, err := s.prepareChatRequest(t.Context(), owner, r); err == nil {
				t.Fatal("historical provider reference bypassed key groups")
			}
		}
	}
}
