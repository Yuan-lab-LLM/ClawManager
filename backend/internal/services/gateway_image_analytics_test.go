package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func imageAnalyticsTestContext(t *testing.T) string {
	t.Helper()
	session, gateway, runtime, mode := "session-images", "gateway-test", "opencode", "lite"
	metadata := `{"operation":"generate","prompt_bytes":20}`
	raw, err := json.Marshal(ImageJobAnalytics{Version: 1, ModelName: "Image model snapshot", Invocation: models.ModelInvocation{
		ProviderType: "openai", RequestedModel: "images", ActualProviderModel: "provider-image",
		SessionID: &session, GatewayID: &gateway, RuntimeType: &runtime, InstanceMode: &mode, RequestPayload: &metadata,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestImageAnalyticsRecordsUseDurableOutcomeNotSettlementStatus(t *testing.T) {
	snapshot := imageAnalyticsTestContext(t)
	now := time.Now().UTC()
	elapsed := 100
	outcome, _ := json.Marshal(ImageJobOutcome{Dispatched: true, ProviderStatus: 200, UsageReported: true, PromptTokens: 10, CompletionTokens: 20, RateLimitTokens: 30, CompletedAt: &now, LatencyMs: &elapsed})
	text := string(outcome)
	instanceID, keyID := 8, 7
	job := &ImageJob{RequestID: "image_completed", State: "completed", UserID: 1, InstanceID: &instanceID, GatewayKeyID: &keyID, ModelID: 100, Charge: .2, AnalyticsContext: &snapshot, AnalyticsOutcome: &text, CreatedAt: now.Add(-time.Second)}
	i, c, err := BuildImageJobRecords(job)
	if err != nil || i.Status != "completed" || c.EstimatedCost != .2 || c.ActualCost != nil || i.TotalTokens != 30 || c.TotalTokens != 30 || *i.LatencyMs != 100 || *c.UserID != 1 || *c.GatewayKeyID != 7 || c.ModelName != "Image model snapshot" || *c.SessionID != "session-images" {
		t.Fatal(i, c, err)
	}
	if c.InputUnitPrice != 0 || c.OutputUnitPrice != 0 || c.InternalCost != 0 {
		t.Fatal("estimate turned into a token price/invoice")
	}
	job.Settled = true
	second, _, err := BuildImageJobRecords(job)
	if err != nil || second.Status != i.Status {
		t.Fatal("settlement changed operation outcome")
	}
}

func TestImageAnalyticsCrashDoesNotInventUsageOrCompletion(t *testing.T) {
	snapshot := imageAnalyticsTestContext(t)
	job := &ImageJob{RequestID: "image_unknown", State: "unknown", UserID: 1, ModelID: 100, Charge: .2, AnalyticsContext: &snapshot, CreatedAt: time.Now().UTC()}
	i, c, err := BuildImageJobRecords(job)
	if err != nil || c == nil || c.EstimatedCost != .2 || c.ActualCost != nil || i.TotalTokens != 0 || i.CompletedAt != nil || i.LatencyMs != nil || !strings.Contains(*i.ResponsePayload, `"outcome_uncertain":true`) || !strings.Contains(*i.ResponsePayload, `"dispatched":null`) || !strings.Contains(*i.ResponsePayload, `"recovered_without_final_outcome":true`) {
		t.Fatal(i, c, err)
	}
	job.State, job.Charge = "failed", 0
	i, c, err = BuildImageJobRecords(job)
	if err != nil || c != nil || i.Status != "failed" {
		t.Fatal("preparation crash created a cost", i, c, err)
	}
	job.State = "running"
	if _, _, err := BuildImageJobRecords(job); err == nil {
		t.Fatal("running job published")
	}
	job.State = "completed"
	invalid := `{"version":999}`
	job.AnalyticsContext = &invalid
	if _, _, err := BuildImageJobRecords(job); err == nil {
		t.Fatal("unknown snapshot version published")
	}
}
