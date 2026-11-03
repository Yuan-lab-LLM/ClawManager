package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestAuditAggregationUsesLatestFailoverAttemptStatus(t *testing.T) {
	now := time.Now().UTC()
	failure := "primary unavailable"
	ttft := 120
	tps := 45.5
	items := []models.ModelInvocation{
		{ID: 1, TraceID: "trace-1", RequestID: "request-1", ProviderType: "openai-compatible", RequestedModel: "auto", ActualProviderModel: "primary", Status: models.ModelInvocationStatusFailed, ErrorMessage: &failure, AttemptNo: 1, CreatedAt: now},
		{ID: 2, TraceID: "trace-1", RequestID: "request-1", ProviderType: "openai-compatible", RequestedModel: "auto", ActualProviderModel: "fallback", Status: models.ModelInvocationStatusCompleted, AttemptNo: 2, TTFTMs: &ttft, TokensPerSecond: &tps, CreatedAt: now.Add(time.Millisecond)},
	}

	result := aggregateInvocationsByTrace(items, nil)
	if len(result) != 1 {
		t.Fatalf("aggregate count = %d", len(result))
	}
	got := result[0]
	if got.Status != models.ModelInvocationStatusCompleted || got.ActualProviderModel != "fallback" || got.ErrorMessage != nil || got.AttemptNo != 2 || got.TTFTMs == nil || *got.TTFTMs != ttft {
		t.Fatalf("latest successful attempt not reflected: %+v", got)
	}
}
