package aigateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

type routingDeploymentStore struct {
	snapshots map[int]services.GatewayDeploymentSnapshot
	err       error
	acquired  []services.GatewayDeploymentAcquireInput
	observed  []services.GatewayDeploymentObservation
}

type routingCostService struct{ records []models.CostRecord }

func (s *routingCostService) RecordCost(record *models.CostRecord) error {
	if record != nil {
		s.records = append(s.records, *record)
	}
	return nil
}
func (*routingCostService) ListCostsByTraceID(string) ([]models.CostRecord, error) {
	return nil, nil
}
func (*routingCostService) ListCostsByUserID(int, int) ([]models.CostRecord, error) {
	return nil, nil
}

func (s *routingDeploymentStore) Snapshots(context.Context, []int) (map[int]services.GatewayDeploymentSnapshot, error) {
	return s.snapshots, s.err
}
func (s *routingDeploymentStore) Acquire(_ context.Context, input services.GatewayDeploymentAcquireInput) (*services.GatewayDeploymentLease, error) {
	s.acquired = append(s.acquired, input)
	return nil, nil
}
func (s *routingDeploymentStore) Observe(_ context.Context, observation services.GatewayDeploymentObservation) error {
	s.observed = append(s.observed, observation)
	return nil
}
func (*routingDeploymentStore) Release(context.Context, *services.GatewayDeploymentLease) error {
	return nil
}

func deploymentRoutingFixture() (*preparedChatRequest, []*models.LLMModel) {
	return &preparedChatRequest{
			sessionID: "session-sticky", userID: 9, requestID: "request-a",
			resolvedModel: &models.LLMModel{ModelGroup: "general"},
			req:           ChatCompletionRequest{MaxTokens: intPtr(256)},
		}, []*models.LLMModel{
			{ID: 1, ModelGroup: "general", DeploymentPriority: 1, MaxConcurrentRequests: 10},
			{ID: 2, ModelGroup: "general", DeploymentPriority: 2, MaxConcurrentRequests: 10},
		}
}

func TestAutoDeploymentRoutingPrefersMateriallyFasterHealthyDeployment(t *testing.T) {
	prepared, candidates := deploymentRoutingFixture()
	now := time.Now().UTC().UnixMilli()
	gateway := &service{deploymentStore: &routingDeploymentStore{snapshots: map[int]services.GatewayDeploymentSnapshot{
		1: {DeploymentID: 1, State: "healthy", EWMATTFTMs: 1800, EWMATokensPerSecond: 15, Samples: 20, LastSuccessMs: now},
		2: {DeploymentID: 2, State: "healthy", EWMATTFTMs: 150, EWMATokensPerSecond: 80, Samples: 20, LastSuccessMs: now},
	}}}

	ordered := gateway.orderAutoDeployments(t.Context(), prepared, candidates)
	if ordered[0].ID != 2 {
		t.Fatalf("selected deployment %d, want faster deployment 2", ordered[0].ID)
	}
}

func TestAutoDeploymentRoutingExcludesCooldownDeployment(t *testing.T) {
	prepared, candidates := deploymentRoutingFixture()
	now := time.Now().UTC().UnixMilli()
	gateway := &service{deploymentStore: &routingDeploymentStore{snapshots: map[int]services.GatewayDeploymentSnapshot{
		1: {DeploymentID: 1, State: "cooldown", CooldownUntilMs: 1 << 62, EWMATTFTMs: 1, EWMATokensPerSecond: 1000, Samples: 20},
		2: {DeploymentID: 2, State: "healthy", EWMATTFTMs: 500, EWMATokensPerSecond: 40, Samples: 20, LastSuccessMs: now},
	}}}

	ordered := gateway.orderAutoDeployments(t.Context(), prepared, candidates)
	if len(ordered) != 1 || ordered[0].ID != 2 {
		t.Fatalf("cooldown deployment remained eligible: %#v", ordered)
	}
}

func TestAutoDeploymentRoutingDoesNotTrustExpiredPerformanceSamples(t *testing.T) {
	prepared, candidates := deploymentRoutingFixture()
	old := time.Now().UTC().Add(-deploymentHealthSampleTTL - time.Minute).UnixMilli()
	now := time.Now().UTC().UnixMilli()
	gateway := &service{deploymentStore: &routingDeploymentStore{snapshots: map[int]services.GatewayDeploymentSnapshot{
		1: {DeploymentID: 1, State: "healthy", EWMATTFTMs: 1, EWMALatencyMs: 1, EWMATokensPerSecond: 10000, Samples: 20, LastSuccessMs: old},
		2: {DeploymentID: 2, State: "healthy", EWMATTFTMs: 400, EWMALatencyMs: 2000, EWMATokensPerSecond: 40, Samples: 20, LastSuccessMs: now},
	}}}

	ordered := gateway.orderAutoDeployments(t.Context(), prepared, candidates)
	if ordered[0].ID != 2 {
		t.Fatalf("expired performance sample remained authoritative: %#v", ordered)
	}
}

func TestAutoDeploymentRoutingUsesRedisSnapshotClock(t *testing.T) {
	prepared, candidates := deploymentRoutingFixture()
	const redisNow = int64(1_000_000)
	gateway := &service{deploymentStore: &routingDeploymentStore{snapshots: map[int]services.GatewayDeploymentSnapshot{
		1: {DeploymentID: 1, State: "healthy", EWMATTFTMs: 1800, EWMATokensPerSecond: 15, Samples: 20, LastSuccessMs: redisNow - 1000, SnapshotAtMs: redisNow},
		2: {DeploymentID: 2, State: "healthy", EWMATTFTMs: 150, EWMATokensPerSecond: 80, Samples: 20, LastSuccessMs: redisNow - 1000, SnapshotAtMs: redisNow},
	}}}

	ordered := gateway.orderAutoDeployments(t.Context(), prepared, candidates)
	if ordered[0].ID != 2 {
		t.Fatalf("local clock skew invalidated fresh Redis samples: %#v", ordered)
	}
}

func TestAutoDeploymentRoutingFallsBackToStaticPriorityOnRedisFailure(t *testing.T) {
	prepared, candidates := deploymentRoutingFixture()
	gateway := &service{deploymentStore: &routingDeploymentStore{err: errors.New("redis unavailable")}}

	ordered := gateway.orderAutoDeployments(t.Context(), prepared, candidates)
	if ordered[0].ID != 1 || ordered[1].ID != 2 {
		t.Fatalf("static order changed on Redis failure: %#v", ordered)
	}
}

func TestDeploymentRouteHashIsStickyAcrossRequestIDs(t *testing.T) {
	prepared, _ := deploymentRoutingFixture()
	first := deploymentRouteHash(prepared, 123)
	prepared.requestID = "request-b"
	if second := deploymentRouteHash(prepared, 123); first != second {
		t.Fatalf("same session changed deployment hash: %d != %d", first, second)
	}
}

func TestDeploymentLeaseIdentityIsUniqueAcrossReusedTraceIDs(t *testing.T) {
	store := &routingDeploymentStore{}
	gateway := &service{deploymentStore: store}
	candidate := &models.LLMModel{ID: 3}
	first := &preparedChatRequest{traceID: "shared-trace", req: ChatCompletionRequest{Messages: []ChatMessage{{Role: "user", Content: "one"}}}}
	second := &preparedChatRequest{traceID: "shared-trace", req: ChatCompletionRequest{Messages: []ChatMessage{{Role: "user", Content: "two"}}}}

	if _, err := gateway.beginDeploymentAttempt(t.Context(), first, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.beginDeploymentAttempt(t.Context(), second, candidate); err != nil {
		t.Fatal(err)
	}
	if len(store.acquired) != 2 || store.acquired[0].RequestID == store.acquired[1].RequestID {
		t.Fatalf("lease identities are not unique: %#v", store.acquired)
	}
}

func TestFailedDispatchedAttemptSettlesTokensAndBudget(t *testing.T) {
	store := &routingDeploymentStore{}
	costs := &routingCostService{}
	budget := &routingBudgetStore{}
	finalizedTokens := 0
	prepared := &preparedChatRequest{
		traceID: "trace-accounting", requestID: "request-accounting", userID: 1, userIDPtr: intPtr(1),
		sessionID: "session-accounting", sessionIDPtr: stringPtr("session-accounting"),
		req: ChatCompletionRequest{
			Messages:         []ChatMessage{{Role: "user", Content: "hello"}},
			BudgetController: services.NewGatewayBudgetController(budget, 1),
			UsageFinalizer: func(tokens int) error {
				finalizedTokens = tokens
				return nil
			},
		},
	}
	candidate := &models.LLMModel{ID: 4, DisplayName: "provider", ProviderType: models.ProviderTypeOpenAICompatible, Currency: "USD", InputPrice: 2, OutputPrice: 4}
	gateway := &service{deploymentStore: store, costRecordService: costs}
	if err := prepared.req.BudgetController.Reserve(t.Context(), 1, services.GatewayBudgetCurrency); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.beginDeploymentAttempt(t.Context(), prepared, candidate); err != nil {
		t.Fatal(err)
	}
	prepared.attemptDispatched = true
	prepared.attemptCompletionTokens = 5
	gateway.finishDeploymentAttempt(prepared, candidate, nil, time.Second, false, true)
	gateway.finalizeFailedAttemptAccounting(prepared)

	wantTokens := prepared.attemptPromptTokens + 5
	if finalizedTokens != wantTokens || budget.finalized <= 0 || len(costs.records) != 1 || costs.records[0].TotalTokens != wantTokens {
		t.Fatalf("failure accounting tokens=%d budget=%f costs=%+v wantTokens=%d", finalizedTokens, budget.finalized, costs.records, wantTokens)
	}
}

func TestNeutralClientErrorRecoversCircuitWithoutPollutingPerformance(t *testing.T) {
	store := &routingDeploymentStore{}
	gateway := &service{deploymentStore: store}
	prepared := &preparedChatRequest{attemptStatusCode: http.StatusBadRequest, attemptTTFTMs: 10, attemptTokensPerSecond: 50}
	candidate := &models.LLMModel{ID: 8}

	gateway.finishDeploymentAttempt(prepared, candidate, nil, time.Second, false, false)
	if len(store.observed) != 1 {
		t.Fatalf("observations = %#v", store.observed)
	}
	got := store.observed[0]
	if !got.Success || got.PenalizeFailure || got.TTFTMs != 0 || got.LatencyMs != 0 || got.TokensPerSecond != 0 {
		t.Fatalf("neutral response observation = %#v", got)
	}
}
