package services

import (
	"context"
	"encoding/json"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Contains server-generated attribution and redacted request metadata only.
// Capture before dispatch, so recovery does not depend on a mutable model row.
type ImageJobAnalytics struct {
	Version    int                    `json:"version"`
	Invocation models.ModelInvocation `json:"invocation"`
	ModelName  string                 `json:"model_name"`
}

type ImageJobOutcome struct {
	Dispatched       bool       `json:"dispatched"`
	Uncertain        bool       `json:"outcome_uncertain"`
	ProviderStatus   int        `json:"provider_status"`
	ArtifactIDs      []string   `json:"artifact_ids"`
	UsageReported    bool       `json:"usage_reported"`
	RateLimitTokens  int        `json:"rate_limit_tokens"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	LatencyMs        *int       `json:"latency_ms,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

func decodeImageAnalytics(raw string) (*ImageJobAnalytics, error) {
	var snapshot ImageJobAnalytics
	if len(raw) > 65536 || json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.Version != 1 || snapshot.ModelName == "" || snapshot.Invocation.ProviderType == "" || snapshot.Invocation.RequestedModel == "" || snapshot.Invocation.ActualProviderModel == "" {
		return nil, ErrImageJobUnavailable
	}
	return &snapshot, nil
}

// BuildImageJobRecords is deterministic over the persisted terminal job. Unknown
// outcomes have no invented token usage, latency, completion time or invoice.
func BuildImageJobRecords(job *ImageJob) (*models.ModelInvocation, *models.CostRecord, error) {
	if job == nil || job.AnalyticsContext == nil || (job.State != "completed" && job.State != "failed" && job.State != "unknown") {
		return nil, nil, ErrImageJobUnavailable
	}
	snapshot, err := decodeImageAnalytics(*job.AnalyticsContext)
	if err != nil {
		return nil, nil, err
	}
	outcome := ImageJobOutcome{Uncertain: job.State == "unknown"}
	recovered := job.AnalyticsOutcome == nil
	if !recovered && (len(*job.AnalyticsOutcome) > 65536 || json.Unmarshal([]byte(*job.AnalyticsOutcome), &outcome) != nil) {
		return nil, nil, ErrImageJobUnavailable
	}
	if outcome.PromptTokens < 0 || outcome.PromptTokens > 10000000 || outcome.CompletionTokens < 0 || outcome.CompletionTokens > 10000000 {
		return nil, nil, ErrImageJobUnavailable
	}
	i := snapshot.Invocation
	i.ID, i.TraceID, i.RequestID = 0, job.RequestID, job.RequestID
	i.UserID, i.InstanceID, i.GatewayKeyID, i.ModelID = &job.UserID, job.InstanceID, job.GatewayKeyID, &job.ModelID
	i.CreatedAt, i.CompletedAt, i.LatencyMs = job.CreatedAt, outcome.CompletedAt, outcome.LatencyMs
	i.TrafficClass, i.AttemptNo, i.IsStreaming = "llm_image", 1, false
	i.PromptTokens, i.CompletionTokens, i.TotalTokens = 0, 0, 0
	i.CachedTokens, i.ReasoningTokens, i.TTFTMs, i.GenerationMs, i.TokensPerSecond = nil, nil, nil, nil, nil
	if outcome.UsageReported {
		i.PromptTokens, i.CompletionTokens = outcome.PromptTokens, outcome.CompletionTokens
		i.TotalTokens = i.PromptTokens + i.CompletionTokens
	}
	i.Status, i.ErrorMessage = models.ModelInvocationStatusCompleted, nil
	if job.State != "completed" {
		i.Status = models.ModelInvocationStatusFailed
		message := "Image request failed; inspect safe outcome metadata"
		if job.State == "unknown" {
			message = "Image provider outcome is unknown; do not regenerate automatically"
		}
		i.ErrorMessage = &message
	}
	var dispatched interface{} = outcome.Dispatched
	if recovered && job.State == "unknown" {
		dispatched = nil // Arm may have committed immediately before the process died.
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"dispatched": dispatched, "outcome_uncertain": outcome.Uncertain, "recovered_without_final_outcome": recovered,
		"provider_status": outcome.ProviderStatus, "error_code": job.ErrorCode, "budget_estimate_charged": job.Charge,
		"price_basis": "admin_per_image_estimate", "artifact_ids": outcome.ArtifactIDs, "usage_reported": outcome.UsageReported,
		"rate_limit_tokens": outcome.RateLimitTokens,
	})
	text := string(payload)
	i.ResponsePayload = &text
	if !outcome.Dispatched && !outcome.Uncertain && job.State != "completed" {
		return &i, nil, nil
	}
	c := &models.CostRecord{
		TraceID: job.RequestID, RequestID: &job.RequestID, UserID: i.UserID, InstanceID: i.InstanceID, GatewayKeyID: i.GatewayKeyID,
		SessionID: i.SessionID, InstanceMode: i.InstanceMode, RuntimeType: i.RuntimeType, GatewayID: i.GatewayID, RuntimePodID: i.RuntimePodID,
		ModelID: i.ModelID, ProviderType: i.ProviderType, ModelName: snapshot.ModelName, Currency: GatewayBudgetCurrency,
		PromptTokens: i.PromptTokens, CompletionTokens: i.CompletionTokens, TotalTokens: i.TotalTokens, EstimatedCost: job.Charge,
		RecordedAt: job.CreatedAt,
	}
	if outcome.CompletedAt != nil {
		c.RecordedAt = *outcome.CompletedAt
	}
	return &i, c, nil
}

// Job row lock + both inserts + marker commit form one transaction. Retrying
// after a crash/ambiguous commit cannot insert a second pair. Do not instantiate
// repositories here: their constructors run schema DDL, which commits MySQL TXs.
func (s *gatewayImageJobService) PublishRecords(ctx context.Context, id string) error {
	return s.sess.WithContext(ctx).Tx(func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT analytics_recorded FROM gateway_image_jobs WHERE request_id=? FOR UPDATE", id)
		if err != nil {
			return err
		}
		var recorded bool
		if err := row.Scan(&recorded); err != nil {
			return err
		}
		if recorded {
			return nil
		}
		var job ImageJob
		if err := tx.Collection("gateway_image_jobs").Find(db.Cond{"request_id": id}).One(&job); err != nil {
			return err
		}
		invocation, cost, err := BuildImageJobRecords(&job)
		if err != nil {
			return err
		}
		res, err := tx.Collection("model_invocations").Insert(invocation)
		if err != nil {
			return err
		}
		invocation.ID = int(res.ID().(int64))
		if cost != nil {
			cost.InvocationID = &invocation.ID
			if _, err := tx.Collection("cost_records").Insert(cost); err != nil {
				return err
			}
		}
		_, err = tx.SQL().ExecContext(ctx, "UPDATE gateway_image_jobs SET analytics_recorded=TRUE,updated_at=? WHERE request_id=?", time.Now().UTC(), id)
		return err
	})
}
