package services

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type feeDetailCostRepo struct {
	*stubSessionUsageCostRepo
	calls int
	err   error
}

func (r *feeDetailCostRepo) ListByTraceID(trace string) ([]models.CostRecord, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.stubSessionUsageCostRepo.ListByTraceID(trace)
}

func feeDetailService(costs *feeDetailCostRepo) *aiObservabilityService {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	instance := 9
	session := "main"
	return &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{aggregates: []repository.InstanceSessionTokenAggregate{{SessionID: session, InvocationCount: 2, TotalTokens: 30, FirstSeenAt: now, LastSeenAt: now}}, invocations: []models.ModelInvocation{
			{ID: 1, TraceID: "retry", InstanceID: &instance, SessionID: &session, RequestedModel: "attempt-a", ActualProviderModel: "actual-a", PromptTokens: 10, TotalTokens: 10, Status: models.ModelInvocationStatusFailed, CreatedAt: now},
			{ID: 2, TraceID: "retry", InstanceID: &instance, SessionID: &session, RequestedModel: "attempt-b", ActualProviderModel: "actual-b", PromptTokens: 20, TotalTokens: 20, Status: models.ModelInvocationStatusCompleted, CreatedAt: now},
		}}, costRepo: costs, chatSessionRepo: &stubSessionUsageChatSessionRepo{}, llmModelRepo: &stubLLMModelRepository{},
	}
}

func TestSessionFeeBreakdownCountsEachCostOnceUsingCostModel(t *testing.T) {
	instance := 9
	other := 10
	session := "main"
	otherSession := "other"
	first := 1
	second := 2
	unknown := 99
	repo := &feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{byTraceID: map[string][]models.CostRecord{"retry": {
		{ID: 1, TraceID: "retry", InstanceID: &instance, SessionID: &session, InvocationID: &first, ModelName: "billed-a", EstimatedCost: 1},
		{ID: 2, TraceID: "retry", InstanceID: &instance, SessionID: &session, InvocationID: &second, ModelName: "billed-b", EstimatedCost: 2},
		{ID: 3, TraceID: "retry", InstanceID: &instance, SessionID: &session, ModelName: "legacy", EstimatedCost: 3},
		{ID: 4, TraceID: "retry", InstanceID: &other, SessionID: &session, ModelName: "wrong-instance", EstimatedCost: 40},
		{ID: 5, TraceID: "retry", InstanceID: &instance, SessionID: &otherSession, ModelName: "wrong-session", EstimatedCost: 50},
		{ID: 6, TraceID: "retry", InstanceID: &instance, SessionID: &session, InvocationID: &unknown, ModelName: "unknown-invocation", EstimatedCost: 60},
	}}}}
	result, err := feeDetailService(repo).GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]float64{}
	tokens := 0
	for _, row := range result.ModelBreakdown {
		totals[row.Label] = row.EstimatedCost
		tokens += row.TotalTokens
	}
	if totals["billed-a"] != 1 || totals["billed-b"] != 2 || totals["legacy"] != 3 || totals["actual-a"] != 0 || totals["actual-b"] != 0 || tokens != 30 {
		t.Fatalf("cost identity or invocation token semantics changed: %+v", result.ModelBreakdown)
	}
	total := 0.0
	for _, v := range totals {
		total += v
	}
	if total != 6 {
		t.Fatalf("expected each eligible cost once, got %v (%+v)", total, totals)
	}
	if repo.calls != 1 {
		t.Fatalf("same trace fetched %d times", repo.calls)
	}
	if len(result.RecentTraces) != 2 || result.InvocationCount != 2 {
		t.Fatalf("invocation semantics changed: %+v", result)
	}
}

func TestSessionFeeBreakdownPropagatesCostLookupFailure(t *testing.T) {
	repo := &feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{}, err: errors.New("fee lookup unavailable")}
	_, err := feeDetailService(repo).GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
	if err == nil || !strings.Contains(err.Error(), "fee lookup unavailable") {
		t.Fatalf("fee lookup failure must be returned: %v", err)
	}
}

type feeAggregateInvocationRepo struct {
	*stubSessionUsageInvocationRepo
	err error
}

func (r *feeAggregateInvocationRepo) AggregateModelsByInstanceSession(int, string, repository.SessionUsageFilter) ([]models.ModelInvocation, error) {
	return []models.ModelInvocation{{ActualProviderModel: "actual-a", TotalTokens: 100}}, r.err
}

type feeAggregateCostRepo struct {
	*feeDetailCostRepo
	err error
}

func (r *feeAggregateCostRepo) AggregateModelCostByInstanceSession(int, string, repository.SessionUsageFilter) ([]models.CostRecord, error) {
	return []models.CostRecord{{ModelName: "billed-a", EstimatedCost: 5}}, r.err
}

func TestSessionFeeBreakdownUsesFullWindowSQLAggregates(t *testing.T) {
	costs := &feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{}}
	service := feeDetailService(costs)
	service.invocationRepo = &feeAggregateInvocationRepo{stubSessionUsageInvocationRepo: service.invocationRepo.(*stubSessionUsageInvocationRepo)}
	service.costRepo = &feeAggregateCostRepo{feeDetailCostRepo: costs}
	result, err := service.GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	totalTokens := 0
	totalCost := 0.0
	for _, row := range result.ModelBreakdown {
		totalTokens += row.TotalTokens
		totalCost += row.EstimatedCost
	}
	if totalTokens != 100 || totalCost != 5 || len(result.RecentTraces) != 2 || costs.calls != 0 {
		t.Fatalf("detail must use full-window aggregates and retain recent traces: %+v, cost reads %d", result, costs.calls)
	}
}

func TestSessionFeeBreakdownPropagatesAggregateFailure(t *testing.T) {
	costs := &feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{}}
	service := feeDetailService(costs)
	service.costRepo = &feeAggregateCostRepo{feeDetailCostRepo: costs, err: errors.New("aggregate unavailable")}
	_, err := service.GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
	if err == nil || !strings.Contains(err.Error(), "aggregate unavailable") {
		t.Fatalf("aggregate failure must be returned: %v", err)
	}
}

func TestSessionFeeBreakdownReturnsEveryUsedIdentity(t *testing.T) {
	for _, count := range []int{1, 3, 6} {
		t.Run(fmt.Sprintf("%d-billed-models", count), func(t *testing.T) {
			costs := &feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{byTraceID: map[string][]models.CostRecord{}}}
			service := feeDetailService(costs)
			invocations := service.invocationRepo.(*stubSessionUsageInvocationRepo)
			invocations.invocations = nil
			totalCost := float64(count * (count + 1) / 2)
			costs.aggregates = []repository.InstanceSessionCostAggregate{{SessionID: "main", EstimatedCost: totalCost, Currency: "USD"}}
			invocations.aggregates[0].TotalTokens = count * 10
			for id := 1; id <= count; id++ {
				trace := fmt.Sprintf("trace-%d", id)
				invocations.invocations = append(invocations.invocations, models.ModelInvocation{ID: id, TraceID: trace, ActualProviderModel: fmt.Sprintf("provider-%d", id), TotalTokens: 10})
				costs.byTraceID[trace] = []models.CostRecord{{ID: id, TraceID: trace, ModelName: fmt.Sprintf("billed-%d", id), EstimatedCost: float64(id), Currency: "USD"}}
			}
			service.llmModelRepo = &stubLLMModelRepository{active: []models.LLMModel{{DisplayName: "unused-active"}}}
			result, err := service.GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
			if err != nil {
				t.Fatal(err)
			}
			rows := map[string]CostBreakdownItem{}
			tokens, fees := 0, 0.0
			for _, row := range result.ModelBreakdown {
				rows[row.Label] = row
				tokens += row.TotalTokens
				fees += row.EstimatedCost
			}
			if len(rows) != 2*count || tokens != result.TotalTokens || fees != result.EstimatedCost {
				t.Fatalf("all used identities must match header without padding: %+v; header tokens=%d fees=%v", result.ModelBreakdown, result.TotalTokens, result.EstimatedCost)
			}
			if _, found := rows["unused-active"]; found {
				t.Fatal("unused catalog identity padded into session detail")
			}
			for id := 1; id <= count; id++ {
				if rows[fmt.Sprintf("provider-%d", id)].TotalTokens != 10 || rows[fmt.Sprintf("billed-%d", id)].EstimatedCost != float64(id) {
					t.Fatalf("lost own-model attribution: %+v", rows)
				}
			}
		})
	}
}

func TestSessionFeeBreakdownRejectsMixedFallbackCurrency(t *testing.T) {
	for _, sameModel := range []bool{true, false} {
		t.Run(fmt.Sprintf("same-model-%v", sameModel), func(t *testing.T) {
			otherModel := "billed-a"
			if !sameModel {
				otherModel = "billed-b"
			}
			costs := &feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{byTraceID: map[string][]models.CostRecord{"retry": {
				{ID: 1, ModelName: "billed-a", EstimatedCost: 1, Currency: "USD"},
				{ID: 2, ModelName: otherModel, EstimatedCost: 10, Currency: "EUR"},
			}}}}
			result, err := feeDetailService(costs).GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
			if !errors.Is(err, repository.ErrMixedCostCurrencies) || result != nil {
				t.Fatalf("mixed currencies must return an explicit error without a partial detail: %+v %v", result, err)
			}
		})
	}
}

type feeCurrencyAggregateRepo struct {
	*feeDetailCostRepo
	rows []models.CostRecord
}

func (r *feeCurrencyAggregateRepo) AggregateModelCostByInstanceSession(int, string, repository.SessionUsageFilter) ([]models.CostRecord, error) {
	return r.rows, nil
}

func TestSessionFeeBreakdownRejectsMixedAggregateCurrency(t *testing.T) {
	service := feeDetailService(&feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{}})
	service.costRepo = &feeCurrencyAggregateRepo{feeDetailCostRepo: service.costRepo.(*feeDetailCostRepo), rows: []models.CostRecord{
		{ModelName: "billed-a", EstimatedCost: 1, Currency: "USD"}, {ModelName: "billed-b", EstimatedCost: 10, Currency: "EUR"},
	}}
	result, err := service.GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
	if !errors.Is(err, repository.ErrMixedCostCurrencies) || result != nil {
		t.Fatalf("mixed aggregate currencies must return no detail: %+v %v", result, err)
	}
}
