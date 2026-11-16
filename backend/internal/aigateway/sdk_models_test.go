package aigateway

import (
	"errors"
	"reflect"
	"testing"

	"clawreef/internal/models"
)

func sdkModelNames(t *testing.T, s *service, userID int, groups []string) []string {
	t.Helper()
	lister, ok := any(s).(interface {
		ListSDKModelsForUser(int, []string) ([]AvailableModel, error)
	})
	if !ok {
		t.Fatal("gateway lacks an owner-scoped SDK model catalog")
	}
	items, err := lister.ListSDKModelsForUser(userID, groups)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.DisplayName)
	}
	return names
}

func TestSDKModelsScopeAndRuntimeIDs(t *testing.T) {
	persisted := `[{"id":"deepseek-v4.1-flash"},{"id":"glm-5.3-flash"}]`
	s := &service{modelRepo: &stubLLMModelRepository{active: []models.LLMModel{
		{ID: 1, DisplayName: "Managed", ProviderModelName: "deepseek-v4.1-flash", ProviderModelsJSON: &persisted, ModelGroup: "allowed", VisibilityScope: "selected_users", AllowedUserIDs: []int{7}, IsActive: true},
		{ID: 2, DisplayName: "Hidden", ProviderModelName: "hidden-model", VisibilityScope: "selected_users", AllowedUserIDs: []int{8}, ModelGroup: "allowed", IsActive: true},
		{ID: 3, DisplayName: "Other group", ProviderModelName: "other-model", ModelGroup: "other", IsActive: true},
		{ID: 4, DisplayName: "Image", ProviderModelName: "image-model", ModelKind: models.ModelKindImage, ModelGroup: "allowed", IsActive: true},
		{ID: 5, DisplayName: "Duplicate deployment", ProviderModelName: "deepseek-v4.1-flash", ModelGroup: "allowed", IsActive: true},
	}}}
	s.expandedModelCatalog = &availableExpandedModelCatalogStub{active: []models.LLMModel{{ID: 99, ProviderModelName: "unapproved-discovery", IsActive: true}}}
	if got := sdkModelNames(t, s, 7, []string{"allowed"}); !reflect.DeepEqual(got, []string{"Auto", "deepseek-v4.1-flash", "glm-5.3-flash"}) {
		t.Fatalf("SDK models leaked or differed from runtime identities: %v", got)
	}
	if got := sdkModelNames(t, s, 8, []string{"allowed"}); !reflect.DeepEqual(got, []string{"Auto", "hidden-model", "deepseek-v4.1-flash"}) {
		t.Fatalf("SDK catalog reused another owner's grants: %v", got)
	}
	if got := sdkModelNames(t, s, 7, []string{"missing"}); len(got) != 0 {
		t.Fatalf("disallowed groups exposed Auto: %v", got)
	}
	legacy, err := s.ListAvailableModelsForUser(7, []string{"allowed"})
	if err != nil || len(legacy) != 1 || legacy[0].DisplayName != "Auto" {
		t.Fatalf("legacy Auto-only choices changed: %v %v", legacy, err)
	}
}

type sdkModelsFailingRepository struct{ *stubLLMModelRepository }

func (*sdkModelsFailingRepository) ListActive() ([]models.LLMModel, error) {
	return nil, errors.New("catalog unavailable")
}

func TestSDKModelsEmptyDeniedAndErrors(t *testing.T) {
	s := &service{modelRepo: &stubLLMModelRepository{active: []models.LLMModel{{ID: 1, DisplayName: "Hidden", ProviderModelName: "hidden", VisibilityScope: "selected_users", AllowedUserIDs: []int{8}, IsActive: true}}}}
	if got := sdkModelNames(t, s, 7, nil); len(got) != 0 {
		t.Fatalf("hidden models exposed: %v", got)
	}
	s.modelRepo = &stubLLMModelRepository{}
	if got := sdkModelNames(t, s, 7, nil); len(got) != 0 {
		t.Fatalf("empty catalog exposed Auto: %v", got)
	}
	s.modelRepo = &stubLLMModelRepository{active: []models.LLMModel{{ID: 1, ProviderModelName: "image-model", ModelKind: models.ModelKindImage, IsActive: true}}}
	if got := sdkModelNames(t, s, 7, nil); len(got) != 0 {
		t.Fatalf("image-only catalog exposed chat models: %v", got)
	}
	s.modelRepo = &sdkModelsFailingRepository{}
	lister, ok := any(s).(interface {
		ListSDKModelsForUser(int, []string) ([]AvailableModel, error)
	})
	if !ok {
		t.Fatal("gateway lacks an owner-scoped SDK model catalog")
	}
	if _, err := lister.ListSDKModelsForUser(7, nil); err == nil {
		t.Fatal("catalog failure became a successful empty list")
	}
}
