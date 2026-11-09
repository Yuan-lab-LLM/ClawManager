package services

import (
	"encoding/json"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Only overlay a failed attempt when a structured, matching failover event
// exists. A trace can be reused across several agent calls, so trace alone is
// not enough: require the request, instance and failed attempt to match.
func applyLatestStreamRetryStatus(summary *InstanceSessionUsageSummary, latest models.ModelInvocation, repo repository.AuditEventRepository, now time.Time) {
	if latest.Status != models.ModelInvocationStatusFailed || !latest.IsStreaming || repo == nil || latest.RequestID == "" {
		return
	}
	events, err := repo.ListByTraceID(latest.TraceID)
	if err != nil {
		return
	}
	var retry *models.AuditEvent
	nextModel := ""
	for i := range events {
		event := &events[i]
		if event.CreatedAt.Before(latest.CreatedAt.Truncate(time.Second)) {
			continue
		}
		if event.RequestID == nil || *event.RequestID != latest.RequestID || event.InstanceID == nil || latest.InstanceID == nil || *event.InstanceID != *latest.InstanceID {
			continue
		}
		if event.EventType == "gateway.request.stream_failed" {
			summary.LatestModelStatus = "request_failed"
			return
		}
		if event.EventType != "gateway.deployment.failover" || event.Details == nil {
			continue
		}
		var details struct {
			FailedAttempt int    `json:"failed_attempt"`
			NextModel     string `json:"next_model"`
		}
		if json.Unmarshal([]byte(*event.Details), &details) != nil || details.FailedAttempt != latest.AttemptNo || details.NextModel == "" {
			continue
		}
		if retry == nil || event.ID > retry.ID {
			retry, nextModel = event, details.NextModel
		}
	}
	if retry == nil {
		return
	}
	// No heartbeat is available for a crashed process. Never display an old
	// failover indefinitely as a request that is definitely still running.
	if now.Sub(retry.CreatedAt) > 10*time.Minute {
		summary.LatestModelStatus = "retry_unknown"
		return
	}
	summary.LatestModelStatus = "retrying"
	summary.LatestActualModel = nextModel
	summary.LatestModelAt = &retry.CreatedAt
	// These measurements belong to the failed model, not the replacement.
	summary.LatestLatencyMs = nil
	summary.LatestTTFTMs = nil
}
