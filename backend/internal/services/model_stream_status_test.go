package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
)

type streamStatusAuditRepo struct {
	stubSessionUsageAuditRepo
	events []models.AuditEvent
}

func (r *streamStatusAuditRepo) ListByTraceID(string) ([]models.AuditEvent, error) {
	return r.events, nil
}

func TestLatestStreamStatusDistinguishesAttemptAndRequest(t *testing.T) {
	now := time.Now()
	id, otherID := 3, 4
	request, otherRequest := "req", "other"
	details := `{"failed_attempt":1,"next_model":"glm"}`
	latest := models.ModelInvocation{Status: models.ModelInvocationStatusFailed, IsStreaming: true, InstanceID: &id, RequestID: request, AttemptNo: 1}
	retry := models.AuditEvent{ID: 2, InstanceID: &id, RequestID: &request, EventType: "gateway.deployment.failover", Details: &details, CreatedAt: now}
	end := models.AuditEvent{ID: 3, InstanceID: &id, RequestID: &request, EventType: "gateway.request.stream_failed", CreatedAt: now}
	for _, tc := range []struct {
		name   string
		events []models.AuditEvent
		status string
		clock  time.Time
	}{
		{"attempt only", nil, "failed", now},
		{"retrying", []models.AuditEvent{retry}, "retrying", now},
		{"request stopped", []models.AuditEvent{retry, end}, "request_failed", now},
		{"stale retry", []models.AuditEvent{retry}, "retry_unknown", now.Add(11 * time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := 49000
			summary := InstanceSessionUsageSummary{LatestModelStatus: "failed", LatestActualModel: "kimi", LatestLatencyMs: &ms, LatestTTFTMs: &ms}
			applyLatestStreamRetryStatus(&summary, latest, &streamStatusAuditRepo{events: tc.events}, tc.clock)
			if summary.LatestModelStatus != tc.status {
				t.Fatalf("%+v", summary)
			}
			if tc.status == "retrying" && (summary.LatestActualModel != "glm" || summary.LatestLatencyMs != nil || summary.LatestTTFTMs != nil) {
				t.Fatalf("stale timing/model: %+v", summary)
			}
		})
	}
	for _, change := range []func(*models.AuditEvent){
		func(e *models.AuditEvent) { e.RequestID = &otherRequest },
		func(e *models.AuditEvent) { e.InstanceID = &otherID },
		func(e *models.AuditEvent) { e.Details = nil },
		func(e *models.AuditEvent) { value := `{"failed_attempt":2,"next_model":"glm"}`; e.Details = &value },
	} {
		event := retry
		change(&event)
		summary := InstanceSessionUsageSummary{LatestModelStatus: "failed"}
		applyLatestStreamRetryStatus(&summary, latest, &streamStatusAuditRepo{events: []models.AuditEvent{event}}, now)
		if summary.LatestModelStatus != "failed" {
			t.Fatal("unrelated retry leaked across requests or attempts")
		}
	}
	latest.Status = models.ModelInvocationStatusCompleted
	summary := InstanceSessionUsageSummary{LatestModelStatus: "completed"}
	applyLatestStreamRetryStatus(&summary, latest, &streamStatusAuditRepo{events: []models.AuditEvent{retry, end}}, now)
	if summary.LatestModelStatus != "completed" {
		t.Fatal("completed response overwritten by earlier failure")
	}
}
