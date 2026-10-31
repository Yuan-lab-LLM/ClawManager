package aigateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

type imageAutoChecker struct {
	denied map[int]bool
	err    error
}

func (c *imageAutoChecker) CheckModelAccess(_ context.Context, _, id int) error {
	if c.err != nil {
		return c.err
	}
	if c.denied[id] {
		return models.ErrModelUnavailable
	}
	return nil
}

func autoImageRequest(t *testing.T, operation ImageOperation) *ImageOperationRequest {
	t.Helper()
	extra := ""
	if operation == ImageEdit {
		extra = `,"images":[{"image_url":"https://example.com/input.png"}]`
	}
	req, err := ParseImageOperationRequest(operation, "application/json", strings.NewReader(`{"model":"auto","prompt":"draw"`+extra+`}`))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestImageAutoFiltersBeforeRanking(t *testing.T) {
	for _, name := range []string{"owner", "key", "inactive", "chat", "generation", "edit", "price", "nan", "currency", "checker-denied", "secure"} {
		t.Run(name, func(t *testing.T) {
			price := .2
			good := imageRoutingModel()
			good.ImagePricePerImage, good.Currency, good.ImageEditEnabled = &price, "USD", true
			bad := good
			bad.ID, bad.DisplayName, bad.DeploymentPriority = 200, "ineligible", -100
			checker := &imageAutoChecker{denied: map[int]bool{}}
			attr := ChatCompletionRequest{AllowedModelGroups: []string{"basic"}}
			operation := ImageGenerate
			switch name {
			case "owner":
				bad.VisibilityScope, bad.AllowedUserIDs = "selected_users", []int{2}
			case "key":
				bad.ModelGroup = "denied"
			case "inactive":
				bad.IsActive = false
			case "chat":
				bad.ModelKind = "chat"
			case "generation":
				bad.ImageGenerationEnabled = false
			case "edit":
				bad.ImageEditEnabled = false
				operation = ImageEdit
			case "price":
				bad.ImagePricePerImage = nil
			case "nan":
				nan := math.NaN()
				bad.ImagePricePerImage = &nan
			case "currency":
				bad.Currency = "CNY"
			case "checker-denied":
				checker.denied[bad.ID] = true
			case "secure":
				bad.IsSecure = false
			}
			s := routingGateway([]models.LLMModel{bad, good}, &stubAuditEventService{})
			s.modelAccessChecker, s.deploymentStore = checker, services.NewNoopGatewayDeploymentStore()
			if name == "secure" {
				s.riskDetector = routingRiskDetector{services.RiskAnalysis{HighestAction: models.RiskActionRouteSecureModel}}
			}
			got, err := s.resolveImageOperationModel(t.Context(), 1, autoImageRequest(t, operation), attr)
			if err != nil || got.ID != good.ID {
				t.Fatal("ineligible Auto candidate", got, err)
			}
			checker.err = errors.New("database unavailable")
			if _, err := s.resolveImageOperationModel(t.Context(), 1, autoImageRequest(t, operation), attr); err == nil {
				t.Fatal("ACL error failed open")
			}
		})
	}
}

func TestImageAutoRanksPriorityLoadHealthAndFreshLatency(t *testing.T) {
	price := .2
	a := imageRoutingModel()
	a.ImagePricePerImage, a.Currency, a.MaxConcurrentRequests = &price, "USD", 10
	b := a
	b.ID, b.DisplayName = 101, "second-image"
	now := time.Now().UTC().UnixMilli()
	healthy := func(latency float64, inflight int) services.GatewayDeploymentSnapshot {
		return services.GatewayDeploymentSnapshot{State: "healthy", LastSuccessMs: now, SnapshotAtMs: now, EWMALatencyMs: latency, Inflight: inflight}
	}
	snapshots := map[int]services.GatewayDeploymentSnapshot{a.ID: healthy(1000, 9), b.ID: healthy(1200, 0)}
	first := func() int {
		t.Helper()
		got := rankImageDeployments([]*models.LLMModel{&a, &b}, snapshots, nil)
		if len(got) == 0 {
			t.Fatal("no candidate")
		}
		return got[0].ID
	}
	if first() != b.ID {
		t.Fatal("load ignored")
	}
	a.DeploymentPriority = -1
	if first() != a.ID {
		t.Fatal("administrator priority ignored")
	}
	a.DeploymentPriority = 0
	snapshots[a.ID] = healthy(10, 0)
	if first() != a.ID {
		t.Fatal("fresh latency ignored")
	}
	stale := snapshots[a.ID]
	stale.LastSuccessMs = now - int64(10*time.Minute/time.Millisecond)
	snapshots[a.ID] = stale
	if first() != b.ID {
		t.Fatal("stale speed dominates")
	}
	for _, state := range []services.GatewayDeploymentSnapshot{
		{State: "cooldown", CooldownUntilMs: now + 60000, SnapshotAtMs: now},
		{State: "half_open", Inflight: 1},
		{State: "cooldown", CooldownUntilMs: now - 1, Inflight: 1, SnapshotAtMs: now},
		{Inflight: 10},
	} {
		snapshots[a.ID] = state
		if got := rankImageDeployments([]*models.LLMModel{&a, &b}, snapshots, nil); len(got) != 1 || got[0].ID != b.ID {
			t.Fatal("busy/cooldown candidate included", got)
		}
	}
}

func TestImageAutoPinsModelAcrossRetriesAndNeverRecharges(t *testing.T) {
	png := visionTestPNG(t)
	var calls atomic.Int32
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input["model"] != "native-image" {
			t.Error("Auto sent upstream without resolving model", input["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"b64_json": png}}})
	})
	modelsRepo := s.modelRepo.(*stubLLMModelRepository)
	a := modelsRepo.active[0]
	b := a
	b.ID, b.DisplayName, b.ProviderModelName = 101, "other-image", "other-provider-model"
	modelsRepo.active = append(modelsRepo.active, b)
	now := time.Now().UTC().UnixMilli()
	deployments := &routingDeploymentStore{snapshots: map[int]services.GatewayDeploymentSnapshot{
		a.ID: {State: "healthy", EWMALatencyMs: 1000, LastSuccessMs: now}, b.ID: {State: "healthy", EWMALatencyMs: 100000, LastSuccessMs: now},
	}}
	s.deploymentStore = deployments
	checker := &imageAutoChecker{denied: map[int]bool{}}
	s.modelAccessChecker = checker
	bytes, _ := base64.StdEncoding.DecodeString(png)
	s.imageArtifacts = &replayImageArtifacts{data: bytes}
	attr.ImageIdempotencyKey = "auto-pinned"
	req := autoImageRequest(t, ImageGenerate)
	first, err := s.ExecuteImage(t.Context(), 1, req, attr)
	if err != nil {
		t.Fatal(err)
	}
	if first.Accounting["selected_model"] != a.DisplayName || first.Accounting["routing_policy"] != imageAutoPolicy || len(deployments.observed) != 1 || !deployments.observed[0].Success {
		t.Fatal("routing/health missing", first.Accounting, deployments.observed)
	}
	// Redis can be down or rankings reversed: a retry must bypass selection.
	deployments.err = errors.New("Redis down")
	modelsRepo.active[0].DisplayName = "renamed-after-generation"
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	second, err := s.ExecuteImage(t.Context(), 1, req, attr)
	if err != nil || first.TraceID != second.TraceID || second.Accounting["selected_model_id"] != a.ID || second.Accounting["selected_model"] != a.DisplayName || calls.Load() != 1 || budget.finalized != .2 {
		t.Fatal("retry rerouted", second, err, calls.Load())
	}
	checker.denied[a.ID] = true
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	if _, err := s.ExecuteImage(t.Context(), 1, req, attr); !errors.Is(err, models.ErrModelUnavailable) || calls.Load() != 1 {
		t.Fatal("revoked pinned model rerouted", err)
	}
}

func TestImageAutoProviderFailureDoesNotFallback(t *testing.T) {
	var calls atomic.Int32
	s, _, _, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(403) })
	repo := s.modelRepo.(*stubLLMModelRepository)
	other := repo.active[0]
	other.ID, other.DisplayName = 101, "backup-image"
	repo.active = append(repo.active, other)
	store := &routingDeploymentStore{}
	s.deploymentStore = store
	_, err := s.ExecuteImage(t.Context(), 1, autoImageRequest(t, ImageGenerate), attr)
	if err == nil || calls.Load() != 1 || len(store.acquired) != 1 || len(store.observed) != 1 || !store.observed[0].PenalizeFailure || !store.observed[0].ImmediateCooldown {
		t.Fatal("paid fallback or missing health", err, calls.Load(), store.observed)
	}
	store.err = errors.New("redis unavailable")
	if _, err := s.ExecuteImage(t.Context(), 1, autoImageRequest(t, ImageGenerate), attr); err == nil || calls.Load() != 1 {
		t.Fatal("snapshot failure bypassed", err)
	}
}

func TestImageStorageFailureDoesNotPoisonProviderHealth(t *testing.T) {
	png := visionTestPNG(t)
	s, _, _, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"b64_json": png}}})
	})
	s.imageArtifacts.(*imageTestArtifacts).fail = true
	store := &routingDeploymentStore{}
	s.deploymentStore = store
	_, err := s.ExecuteImage(t.Context(), 1, autoImageRequest(t, ImageGenerate), attr)
	if err == nil || len(store.observed) != 1 || !store.observed[0].Success || store.observed[0].PenalizeFailure {
		t.Fatal("storage failure penalized provider", err, store.observed)
	}
}
