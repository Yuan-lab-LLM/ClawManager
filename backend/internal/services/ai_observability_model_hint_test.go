package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestInstanceModelHintAndHistoryPreserveActualModel(t *testing.T) {
	now := time.Now().UTC()
	repo := &stubSessionUsageInvocationRepo{
		aggregates: []repository.InstanceSessionTokenAggregate{{SessionID: "session", TotalTokens: 30, LastSeenAt: now, FirstSeenAt: now}},
		invocations: []models.ModelInvocation{
			{TraceID: "latest", RequestedModel: "auto", ActualProviderModel: "fallback-model", Status: models.ModelInvocationStatusCompleted, TotalTokens: 20, CreatedAt: now},
			{TraceID: "older", RequestedModel: "auto", ActualProviderModel: "other-model", Status: models.ModelInvocationStatusCompleted, TotalTokens: 10, CreatedAt: now.Add(-time.Minute)},
		},
	}
	service := &aiObservabilityService{invocationRepo: repo, costRepo: &stubSessionUsageCostRepo{}, chatSessionRepo: &stubSessionUsageChatSessionRepo{}, llmModelRepo: &stubLLMModelRepository{}}
	usage, err := service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{Page: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Summary.LatestRequestedModel != "auto" || usage.Summary.LatestActualModel != "fallback-model" || usage.Summary.LatestModelStatus != models.ModelInvocationStatusCompleted {
		t.Fatalf("incorrect model hint: %+v", usage.Summary)
	}
	detail, err := service.GetInstanceSessionUsageDetail(9, "session", repository.SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.RecentTraces) != 2 || detail.RecentTraces[0].RequestedModel != "auto" || detail.RecentTraces[0].ActualProviderModel != "fallback-model" {
		t.Fatalf("actual model missing in history: %+v", detail.RecentTraces)
	}
	totals := map[string]int{}
	for _, row := range detail.ModelBreakdown {
		totals[row.Label] = row.TotalTokens
	}
	if totals["fallback-model"] != 20 || totals["other-model"] != 10 || totals["auto"] != 0 {
		t.Fatalf("actual deployments must remain separate: %+v", totals)
	}
}
