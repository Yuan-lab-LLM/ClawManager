package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

type interruptedDispatchRepo struct {
	*stubLLMModelRepository
	denyID int
}

func (r *interruptedDispatchRepo) CheckModelAccess(_ context.Context, _, modelID int) error {
	if modelID == r.denyID {
		return models.ErrModelUnavailable
	}
	return nil
}

type interruptedBindStore struct {
	taskTestStore
	denyID int
}

func (s *interruptedBindStore) Bind(_ context.Context, _ models.GatewayTaskIdentity, modelID int) error {
	if modelID == s.denyID {
		return models.ErrModelUnavailable
	}
	return nil
}

type dispatchBudgetStore struct {
	routingBudgetStore
	settlements int
	releases    int
}

func (s *dispatchBudgetStore) Finalize(ctx context.Context, lease *services.GatewayBudgetLease, amount float64) error {
	s.settlements++
	return s.routingBudgetStore.Finalize(ctx, lease, amount)
}
func (s *dispatchBudgetStore) Release(context.Context, *services.GatewayBudgetLease) error {
	s.releases++
	return nil
}

// Exercise the real entry points, including handler-style budget cleanup. A
// dispatch rejected before any upstream call must not be charged; a rejected
// fallback must settle the previous attempt exactly once.
func TestInterruptedDispatchSettlesPriorAttempts(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, stage := range []string{"access", "bind"} {
			for _, priorAttempt := range []bool{false, true} {
				name := fmtBool(stream) + "/" + stage + "/first"
				denyID := 1
				if priorAttempt {
					name = fmtBool(stream) + "/" + stage + "/fallback"
					denyID = 2
				}
				t.Run(name, func(t *testing.T) {
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						http.Error(w, "temporary failure", http.StatusServiceUnavailable)
					}))
					defer upstream.Close()
					items := routingModels()
					for i := range items {
						items[i].ModelGroup = "basic"
						items[i].BaseURL = upstream.URL
						items[i].ProviderModelName = items[i].DisplayName
					}
					gateway := routingGateway(items, &stubAuditEventService{})
					repo := &interruptedDispatchRepo{stubLLMModelRepository: &stubLLMModelRepository{active: items}}
					gateway.modelRepo = repo
					gateway.httpClient, gateway.streamHTTPClient = upstream.Client(), upstream.Client()
					gateway.deploymentStore = &routingDeploymentStore{}
					costs := &routingCostService{}
					gateway.costRecordService = costs
					budget := &dispatchBudgetStore{}
					req := routingRequest("hello")
					req.Model, req.Stream = "cheap", stream
					req.BudgetController = services.NewGatewayBudgetController(budget, 1)
					finalizations, tokens := 0, 0
					req.UsageFinalizer = func(n int) error { finalizations++; tokens = n; return nil }
					if stage == "access" {
						repo.denyID = denyID
					} else {
						gateway.runtimeTasks = &interruptedBindStore{denyID: denyID}
						req.RuntimeTask = &models.GatewayTaskIdentity{InstanceID: 1, UserID: 1, SessionID: "s", TaskID: "t"}
					}
					var err error
					if stream {
						_, err = gateway.StreamChatCompletions(t.Context(), 1, req, httptest.NewRecorder())
					} else {
						_, _, err = gateway.ChatCompletions(t.Context(), 1, req)
					}
					if !errors.Is(err, models.ErrModelUnavailable) {
						t.Fatalf("expected interruption, got %v", err)
					}
					if err := req.BudgetController.Release(t.Context()); err != nil {
						t.Fatal(err)
					}
					if priorAttempt {
						if calls.Load() != 1 || finalizations != 1 || budget.settlements != 1 || budget.releases != 0 || len(costs.records) != 1 {
							t.Fatalf("calls=%d token settlements=%d budget settlements=%d releases=%d costs=%d", calls.Load(), finalizations, budget.settlements, budget.releases, len(costs.records))
						}
						if tokens <= 0 || tokens != costs.records[0].TotalTokens || budget.finalized <= 0 || budget.finalized != costs.records[0].EstimatedCost {
							t.Fatalf("inconsistent settlement: tokens=%d budget=%f record=%+v", tokens, budget.finalized, costs.records[0])
						}
					} else if calls.Load() != 0 || finalizations != 0 || budget.settlements != 0 || budget.releases != 1 || len(costs.records) != 0 {
						t.Fatalf("undispatched call charged: calls=%d tokens=%d budget=%+v costs=%+v", calls.Load(), tokens, budget, costs.records)
					}
				})
			}
		}
	}
}

type unavailableCapacityStore struct {
	routingDeploymentStore
	acquireErr error
}

func (s *unavailableCapacityStore) Acquire(_ context.Context, input services.GatewayDeploymentAcquireInput) (*services.GatewayDeploymentLease, error) {
	s.acquired = append(s.acquired, input)
	return nil, s.acquireErr
}

func TestDeploymentCapacityFailurePolicy(t *testing.T) {
	for _, limit := range []int{0, 5} {
		for _, missing := range []bool{false, true} {
			gateway := &service{}
			if !missing {
				gateway.deploymentStore = &unavailableCapacityStore{acquireErr: errors.New("redis offline")}
			}
			_, err := gateway.beginDeploymentAttempt(t.Context(), &preparedChatRequest{}, &models.LLMModel{ID: 1, MaxConcurrentRequests: limit})
			if (limit > 0) != errors.Is(err, services.ErrGatewayDeploymentUnavailable) {
				t.Fatalf("limit=%d missing=%v err=%v", limit, missing, err)
			}
		}
	}
	// A healthy store's explicit cooldown/capacity rejection is never bypassed,
	// even when no administrator-configured limit exists.
	gateway := &service{deploymentStore: &unavailableCapacityStore{acquireErr: services.ErrGatewayDeploymentUnavailable}}
	if _, err := gateway.beginDeploymentAttempt(t.Context(), &preparedChatRequest{}, &models.LLMModel{ID: 1}); !errors.Is(err, services.ErrGatewayDeploymentUnavailable) {
		t.Fatalf("explicit rejection bypassed: %v", err)
	}
}

func TestDeploymentCapacityFailureSkipsDispatchAndTriesFallback(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, fallback := range []bool{false, true} {
			t.Run(fmtBool(stream)+"/fallback-"+fmtBool(fallback), func(t *testing.T) {
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var body struct{ Model string }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "capable" {
						t.Errorf("unexpected upstream model: %q, err=%v", body.Model, err)
					}
					http.Error(w, "terminal response", http.StatusBadRequest)
				}))
				defer upstream.Close()
				items := routingModels()
				for i := range items {
					items[i].BaseURL, items[i].ProviderModelName = upstream.URL, items[i].DisplayName
					items[i].MaxConcurrentRequests = 5
				}
				if fallback {
					items[1].MaxConcurrentRequests = 0
				}
				gateway := routingGateway(items, &stubAuditEventService{})
				gateway.httpClient, gateway.streamHTTPClient = upstream.Client(), upstream.Client()
				gateway.costRecordService = &routingCostService{}
				capacity := &unavailableCapacityStore{acquireErr: errors.New("redis offline")}
				gateway.deploymentStore = capacity
				req := routingRequest("hello")
				req.Stream = stream
				var err error
				if stream {
					_, err = gateway.StreamChatCompletions(t.Context(), 1, req, httptest.NewRecorder())
				} else {
					_, _, err = gateway.ChatCompletions(t.Context(), 1, req)
				}
				if len(capacity.acquired) != 2 {
					t.Fatalf("fallback was not tried: %+v err=%v", capacity.acquired, err)
				}
				if fallback {
					if err != nil || calls.Load() != 1 {
						t.Fatalf("fallback calls=%d err=%v", calls.Load(), err)
					}
				} else if !errors.Is(err, services.ErrGatewayDeploymentUnavailable) || calls.Load() != 0 {
					t.Fatalf("capacity bypass: calls=%d err=%v", calls.Load(), err)
				}
			})
		}
	}
}
