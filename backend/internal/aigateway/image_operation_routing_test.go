package aigateway

import (
	"errors"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func imageRoutingModel() models.LLMModel {
	return models.LLMModel{ID: 100, DisplayName: "image-only", ProviderModelName: "native-image", ModelGroup: "basic", ModelKind: models.ModelKindImage,
		IsActive: true, ProviderType: models.ProviderTypeOpenAICompatible, ImageGenerationEnabled: true, ImageEditEnabled: false,
		DeploymentPriority: 0, QualityTier: 4, IsSecure: true, ContextWindowTokens: 1}
}

func TestImageModelNeverEntersChatRoutingOrCatalog(t *testing.T) {
	items := append([]models.LLMModel{imageRoutingModel()}, routingModels()...)
	s := routingGateway(items, &stubAuditEventService{})
	if _, err := s.resolveRequestedModel("image-only"); err == nil {
		t.Fatal("explicit image alias accepted for chat")
	}
	for _, name := range []string{"auto", "basic", "cheap"} {
		m, err := s.resolveRequestedModel(name)
		if err != nil || m.ModelKind == "image" {
			t.Fatalf("%s selected image: %+v %v", name, m, err)
		}
	}
	for _, m := range s.deploymentCandidates(&items[1]) {
		if m.ModelKind == "image" {
			t.Fatal("image entered chat fallback")
		}
	}
	if candidates := s.deploymentCandidates(&items[0]); len(candidates) != 0 {
		t.Fatal("image selected as chat fallback seed")
	}
	req := routingRequest("hello")
	if routingCandidateAllowed(&items[0], req, classifyRoutingDifficulty(req.Messages)) {
		t.Fatal("image accepted by Auto")
	}
	prepared, err := s.prepareChatRequest(t.Context(), 1, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range prepared.deploymentCandidates {
		if m.ModelKind == "image" {
			t.Fatal("image entered Auto fallback")
		}
	}
	catalog, err := s.RuntimeCatalog(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range catalog {
		if m.ID == "image-only" || m.ContextWindow == 1 {
			t.Fatal("image contaminated chat catalog", m)
		}
	}
	onlyImages := routingGateway(items[:1], &stubAuditEventService{})
	if got, err := onlyImages.ListAvailableModels(); err != nil || len(got) != 0 {
		t.Fatal("phantom chat auto", got, err)
	}
	if got, err := onlyImages.ListAvailableModelsForUser(1, nil); err != nil || len(got) != 0 {
		t.Fatal("phantom user auto", got, err)
	}
	if got, err := onlyImages.RuntimeCatalog(1, nil); err != nil || len(got) != 0 {
		t.Fatal("phantom runtime auto", got, err)
	}
}

func TestImagePreflightOwnerKeyAndCapabilities(t *testing.T) {
	model := imageRoutingModel()
	model.VisibilityScope, model.AllowedUserIDs = "selected_users", []int{1}
	s := routingGateway([]models.LLMModel{model}, &stubAuditEventService{})
	req, err := ParseImageOperationRequest(ImageGenerate, "application/json", strings.NewReader(`{"model":"image-only","prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.resolveImageOperationModel(t.Context(), 1, req, ChatCompletionRequest{AllowedModelGroups: []string{"basic"}}); err != nil || got.ID != model.ID {
		t.Fatal(got, err)
	}
	for _, owner := range []int{0, 2} {
		if _, err := s.resolveImageOperationModel(t.Context(), owner, req, ChatCompletionRequest{}); !errors.Is(err, models.ErrModelUnavailable) {
			t.Fatal("owner access bypass", err)
		}
	}
	if _, err := s.resolveImageOperationModel(t.Context(), 1, req, ChatCompletionRequest{AllowedModelGroups: []string{"other"}}); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("key groups bypass", err)
	}
	for _, op := range []ImageOperation{ImageEdit, "unknown"} {
		copy := *req
		copy.operation = op
		if copy.ValidateModel(&model) == nil {
			t.Fatal("generation implied edit", op)
		}
	}
	for _, name := range []string{"auto", "Auto", "basic", "native-image", "missing"} {
		copy := *req
		copy.model = name
		if _, err := s.resolveImageOperationModel(t.Context(), 1, &copy, ChatCompletionRequest{}); err == nil {
			t.Fatal("implicit image routing allowed", name)
		}
	}
	for _, modify := range []func(*models.LLMModel){
		func(m *models.LLMModel) {
			m.ImageGenerationEnabled = false
			yes := true
			m.VisionSupportOverride = &yes
		},
		func(m *models.LLMModel) { m.ModelKind = "chat" },
		func(m *models.LLMModel) { m.ProviderType = models.ProviderTypeLocal; m.ProtocolType = "anthropic" },
		func(m *models.LLMModel) { m.IsActive = false },
	} {
		candidate := model
		modify(&candidate)
		gateway := routingGateway([]models.LLMModel{candidate}, &stubAuditEventService{})
		if _, err := gateway.resolveImageOperationModel(t.Context(), 1, req, ChatCompletionRequest{}); err == nil {
			t.Fatal("unsupported image model", candidate)
		}
	}
	s.runtimeTasks = &taskTestStore{state: models.GatewayTaskState{Revoked: true}}
	if _, err := s.resolveImageOperationModel(t.Context(), 1, req, ChatCompletionRequest{RuntimeTask: &models.GatewayTaskIdentity{}}); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("revoked task resumed through image operation", err)
	}
}

func TestImageWireAdapterRequiresExplicitCapability(t *testing.T) {
	req, err := ParseImageOperationRequest(ImageGenerate, "application/json", strings.NewReader(`{"model":"image-only","prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	model := imageRoutingModel()
	model.ImageGenerationEnabled = false
	if _, _, err := req.ProviderBody(&model); !errors.Is(err, ErrInvalidImageOperation) {
		t.Fatal("wire adapter bypassed operation switch", err)
	}
}
