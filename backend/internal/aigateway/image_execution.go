package aigateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

const imageTrafficClass = "llm_image"
const maxImageResponseBytes = 64 << 20

// Optional extension, leaving existing chat-only service implementations intact.
type ImagesService interface {
	ExecuteImage(context.Context, int, *ImageOperationRequest, ChatCompletionRequest) (*ImageExecutionResponse, error)
	LoadImageArtifact(context.Context, services.ImageArtifactOwner, string, []string) (*services.ImageArtifact, error)
}

type ImageExecutionError struct {
	Status        int
	Code, Message string
}

func (e *ImageExecutionError) Error() string { return e.Message }

// Carries only a server-generated identifier, while preserving errors.Is/As
// so an ACL revocation remains a non-retryable permission error.
type ImageRequestError struct {
	RequestID string
	Err       error
}

func (e *ImageRequestError) Error() string { return e.Err.Error() }
func (e *ImageRequestError) Unwrap() error { return e.Err }
func imageError(status int, code, message string) error {
	return &ImageExecutionError{status, code, message}
}

type imageResultItem struct {
	Base64        string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	ArtifactID    string `json:"artifact_id,omitempty"`
	URL           string `json:"url,omitempty"`
}
type imageUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
type ImageExecutionResponse struct {
	Created    int64                  `json:"created"`
	Data       []imageResultItem      `json:"data"`
	Usage      *imageUsage            `json:"usage,omitempty"`
	Accounting map[string]interface{} `json:"clawmanager"`
	TraceID    string                 `json:"-"`
}

func WithImageArtifacts(store services.GatewayImageArtifactService) ServiceOption {
	return func(s *service) { s.imageArtifacts = store }
}

// EstimateImageRequestTokens is a platform admission estimate, not provider
// usage. Unknown usage retains this reservation; it is never logged as actual.
func EstimateImageRequestTokens(r *ImageOperationRequest) int {
	if r == nil {
		return 0
	}
	metadata := r.AuditMetadata()
	return len(r.prompt) + metadata["input_images"].(int)*8192 + r.count*32768
}

func (s *service) ExecuteImage(ctx context.Context, userID int, req *ImageOperationRequest, attribution ChatCompletionRequest) (result *ImageExecutionResponse, retErr error) {
	if req == nil {
		return nil, ErrInvalidImageOperation
	}
	if req.streaming {
		return nil, imageError(400, "image_streaming_not_supported", "Image streaming is not enabled; use stream=false")
	}
	var responseFormat string
	_ = json.Unmarshal(req.fields["response_format"], &responseFormat)
	if responseFormat != "" && responseFormat != "b64_json" {
		return nil, imageError(400, "image_url_output_not_supported", "Use b64_json output; upstream image URL downloads are not enabled")
	}
	if s.imageArtifacts == nil || s.imageJobs == nil || s.auditEventService == nil || s.invocationService == nil || s.costRecordService == nil || s.riskDetector == nil || s.deploymentStore == nil {
		return nil, imageError(503, "image_governance_unavailable", "Image governance dependencies are unavailable")
	}
	local := *s
	if local.modelAccessChecker == nil {
		local.modelAccessChecker, _ = s.modelRepo.(repository.ModelAccessChecker)
	}
	if local.modelAccessChecker == nil {
		return nil, imageError(503, "image_authorization_unavailable", "Image authorization is unavailable")
	}
	s = &local
	if !ValidateImageIdempotencyKey(attribution.ImageIdempotencyKey) {
		return nil, imageError(400, "invalid_idempotency_key", "Idempotency-Key must contain at most 200 visible ASCII characters")
	}
	if err := s.checkRuntimeTask(ctx, attribution); err != nil {
		return nil, err
	}
	ctx, deadlineCancel := context.WithTimeout(ctx, providerRequestTimeout)
	defer deadlineCancel()
	owner := services.ImageArtifactOwner{UserID: userID, InstanceID: attribution.InstanceID, GatewayKeyID: attribution.GatewayKeyID}
	if attribution.ImageIdempotencyKey != "" {
		job, err := s.imageJobs.FindIdempotent(ctx, owner, attribution.ImageIdempotencyKey)
		if err != nil {
			return nil, err
		}
		if job != nil {
			return s.replayPinnedImageJob(ctx, owner, job, req, attribution)
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	requestID := "image_" + hex.EncodeToString(nonce[:])
	traceID := requestID
	attribution.TraceID, attribution.RequestID = &traceID, &requestID
	model, err := s.resolveImageOperationModel(ctx, userID, req, attribution)
	if err != nil {
		return nil, err
	}
	if model.ImagePricePerImage == nil || math.IsNaN(*model.ImagePricePerImage) || math.IsInf(*model.ImagePricePerImage, 0) || *model.ImagePricePerImage < 0 || *model.ImagePricePerImage > 1000000 {
		return nil, imageError(400, "image_price_required", "Configure the model's per-image budget estimate before use")
	}
	if !strings.EqualFold(strings.TrimSpace(model.Currency), services.GatewayBudgetCurrency) {
		return nil, services.ErrGatewayBudgetCurrencyMismatch
	}
	if attribution.GatewayKeyID != nil && attribution.BudgetController == nil {
		return nil, imageError(503, "image_budget_unavailable", "Image budget accounting is unavailable")
	}
	idempotencyKey := attribution.ImageIdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = requestID
	}
	start := time.Now()
	metadata := req.AuditMetadata()
	metadata["price_basis"] = "admin_per_image_estimate"
	metadata["unit_price"] = *model.ImagePricePerImage
	metadata["currency"] = services.GatewayBudgetCurrency
	metadata["pixel_risk_scan"] = false
	metadata["routing_policy"] = imageRoutingPolicy(req.model)
	metadata["selected_model"] = model.DisplayName
	metadata["selected_model_id"] = model.ID
	requestMeta, _ := json.Marshal(metadata)
	requestMetaText := string(requestMeta)
	invocation := models.ModelInvocation{TraceID: traceID, RequestID: requestID, SessionID: attribution.SessionID, UserID: &userID, ModelID: &model.ID,
		ProviderType: model.ProviderType, RequestedModel: req.model, ActualProviderModel: model.ProviderModelName, TrafficClass: imageTrafficClass,
		RequestPayload: &requestMetaText, AttemptNo: 1, CreatedAt: start.UTC()}
	applyInvocationRuntimeAttribution(&invocation, attribution)
	snapshot, err := json.Marshal(services.ImageJobAnalytics{Version: 1, Invocation: invocation, ModelName: model.DisplayName})
	if err != nil {
		return nil, err
	}
	snapshotText := string(snapshot)
	job, claimed, err := s.imageJobs.Claim(ctx, &services.ImageJob{RequestID: requestID, ScopeHash: services.ImageJobScope(owner), IdempotencyHash: services.ImageIdempotencyHash(idempotencyKey), RequestHash: req.fingerprint(attribution), UserID: userID, InstanceID: owner.InstanceID, GatewayKeyID: owner.GatewayKeyID, ModelID: model.ID, Estimate: float64(req.count) * (*model.ImagePricePerImage), AnalyticsContext: &snapshotText})
	if err != nil {
		return nil, err
	}
	if !claimed {
		return s.replayPinnedImageJob(ctx, owner, job, req, attribution)
	}
	ctx, stopGuard, err := guardModelCall(ctx, s.taskAccessChecker(attribution), userID, model.ID)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.imageJobs.Finish(cleanupCtx, requestID, "failed", 0, nil, imageFailureCode(err))
		_ = s.imageJobs.Settle(cleanupCtx, requestID)
		_ = s.imageJobs.PublishRecords(cleanupCtx, requestID)
		return nil, &ImageRequestError{RequestID: requestID, Err: err}
	}
	defer stopGuard()
	audit := &models.AuditEvent{TraceID: traceID, RequestID: &requestID, UserID: &userID, SessionID: attribution.SessionID,
		EventType: "gateway.image.received", TrafficClass: imageTrafficClass, Severity: models.AuditSeverityInfo, Message: "Image request accepted for governance checks", Details: &requestMetaText}
	applyAuditRuntimeAttribution(audit, attribution)
	if err := s.auditEventService.RecordEvent(audit); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.imageJobs.Finish(cleanupCtx, requestID, "failed", 0, nil, "image_audit_unavailable")
		_ = s.imageJobs.Settle(cleanupCtx, requestID)
		_ = s.imageJobs.PublishRecords(cleanupCtx, requestID)
		return nil, &ImageRequestError{RequestID: requestID, Err: imageError(503, "image_audit_unavailable", "Image audit persistence is unavailable")}
	}
	dispatched, uncertain := false, false
	providerSucceeded := false
	var providerStarted time.Time
	providerLatency := 0
	providerStatus := 0
	charge, tokens := 0.0, 0
	var usage *imageUsage
	var artifactIDs []string
	var created int64
	// Accounting uses an independent bounded context even if the caller leaves or
	// access is revoked. Unknown provider outcomes are not silently refunded.
	defer func() {
		settleCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		state := "failed"
		if uncertain {
			state = "unknown"
		} else if retErr == nil && result != nil {
			state = "completed"
		}
		saved, _ := json.Marshal(savedImageResult{Created: created, ArtifactIDs: artifactIDs, Usage: usage})
		savedText := string(saved)
		now := time.Now().UTC()
		elapsed := int(time.Since(start).Milliseconds())
		outcome := &services.ImageJobOutcome{Dispatched: dispatched, Uncertain: uncertain, ProviderStatus: providerStatus, ArtifactIDs: artifactIDs,
			UsageReported: usage != nil, RateLimitTokens: tokens, CompletedAt: &now, LatencyMs: &elapsed}
		if usage != nil {
			outcome.PromptTokens, outcome.CompletionTokens = usage.InputTokens, usage.OutputTokens
		}
		if err := s.imageJobs.Finish(settleCtx, requestID, state, charge, &savedText, imageFailureCode(retErr), outcome); err != nil {
			retErr = imageError(503, "image_job_persistence_pending", "Image task persistence is pending; do not regenerate automatically")
		} else if err := s.imageJobs.Settle(settleCtx, requestID); err != nil {
			retErr = imageError(503, "image_settlement_pending", "Image result is recorded but budget settlement is pending; do not regenerate automatically")
		}
		if dispatched {
			if attribution.UsageFinalizer != nil {
				if err := attribution.UsageFinalizer(tokens); err != nil {
					retErr = imageError(503, "image_usage_settlement_pending", "Image usage settlement is pending; do not regenerate automatically")
				}
			}
		}
		if err := attribution.BudgetController.Release(settleCtx); err != nil {
			log.Printf("Image budget cleanup pending: %s", requestID)
		}
		if err := s.imageJobs.PublishRecords(settleCtx, requestID); err != nil {
			log.Printf("Image analytics pending: %s", requestID)
			if retErr == nil {
				retErr = imageError(503, "image_record_pending", "Image records await recovery; query the existing request instead of regenerating")
			}
		}
		// Settlement and record writes may take time. Recheck authorization at
		// the last return boundary, not only before storing the generated bytes.
		if retErr == nil {
			retErr = s.taskAccessChecker(attribution).CheckModelAccess(ctx, userID, model.ID)
			if retErr == nil {
				retErr = context.Cause(ctx)
			}
		}
		if retErr != nil {
			result = nil
			retErr = &ImageRequestError{RequestID: requestID, Err: retErr}
		}
	}()
	risk := s.riskDetector.AnalyzeText(req.prompt)
	if risk.HighestAction == models.RiskActionBlock || risk.HighestAction == "require_approval" || (risk.HighestAction == models.RiskActionRouteSecureModel && !model.IsSecure) {
		return nil, imageError(403, "image_policy_denied", "Image prompt was denied by the configured content policy")
	}
	body, contentType, err := req.ProviderBody(model)
	if err != nil {
		return nil, err
	}
	resource := "images/generations"
	if req.operation == ImageEdit {
		resource = "images/edits"
	}
	endpoint, err := url.Parse(strings.TrimRight(model.BaseURL, "/") + "/" + resource)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, imageError(400, "image_endpoint_invalid", "Image provider Base URL is invalid")
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upstream.GetBody = nil // No transport replay of a paid POST.
	upstream.Header.Set("Content-Type", contentType)
	upstream.Header.Set("Accept", "application/json")
	key, err := s.secretRefService.ResolveString(ctx, model.APIKey, model.APIKeySecretRef)
	if err != nil {
		return nil, imageError(503, "image_credentials_unavailable", "Image provider credentials are unavailable")
	}
	if key != nil {
		upstream.Header.Set("Authorization", "Bearer "+*key)
	}
	lease, err := s.deploymentStore.Acquire(ctx, services.GatewayDeploymentAcquireInput{DeploymentID: model.ID, RequestID: requestID, MaxInflight: model.MaxConcurrentRequests})
	if err != nil {
		return nil, err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.deploymentStore.Release(releaseCtx, lease)
		if dispatched {
			penalize := providerStatus == 401 || providerStatus == 403 || providerStatus == 429 || providerStatus >= 500 || (uncertain && context.Cause(ctx) == nil)
			observation := services.GatewayDeploymentObservation{DeploymentID: model.ID, Success: providerSucceeded, PenalizeFailure: penalize, ImmediateCooldown: providerStatus == 401 || providerStatus == 403}
			if providerSucceeded {
				observation.LatencyMs = providerLatency
			}
			_ = s.deploymentStore.Observe(releaseCtx, observation)
		}
	}()
	reserved := float64(req.count) * (*model.ImagePricePerImage)
	if err := attribution.BudgetController.Reserve(ctx, reserved, services.GatewayBudgetCurrency); err != nil {
		return nil, err
	}
	if err := s.bindRuntimeTask(ctx, attribution, model.ID); err != nil {
		return nil, err
	}
	if err := s.taskAccessChecker(attribution).CheckModelAccess(ctx, userID, model.ID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := attribution.BudgetController.HandoffImage(func(lease *services.GatewayBudgetLease) error { return s.imageJobs.Arm(ctx, requestID, lease) }); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := *s.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dispatched, uncertain, charge, tokens = true, true, reserved, EstimateImageRequestTokens(req)
	providerStarted = time.Now()
	response, err := client.Do(upstream)
	if err != nil {
		if errors.Is(context.Cause(ctx), models.ErrModelUnavailable) {
			return nil, models.ErrModelUnavailable
		}
		return nil, imageError(502, "image_outcome_unknown", "Image provider outcome is unknown; budget estimate retained; do not retry automatically")
	}
	defer response.Body.Close()
	providerStatus = response.StatusCode
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case 400, 401, 403, 404, 422, 429:
			charge, tokens, uncertain = 0, 0, false
		}
		return nil, imageError(502, "image_provider_rejected", "Image provider did not return a successful result; inspect the Gateway invocation record before retrying")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxImageResponseBytes+1))
	if err != nil || len(raw) > maxImageResponseBytes {
		return nil, imageError(502, "image_result_invalid", "Image result is incomplete or exceeds Gateway limits; do not retry automatically")
	}
	var parsed ImageExecutionResponse
	if json.Unmarshal(raw, &parsed) != nil || len(parsed.Data) == 0 || len(parsed.Data) > req.count {
		return nil, imageError(502, "image_result_invalid", "Image provider returned an invalid result; do not retry automatically")
	}
	if parsed.Usage != nil && parsed.Usage.InputTokens >= 0 && parsed.Usage.OutputTokens >= 0 && parsed.Usage.TotalTokens > 0 && parsed.Usage.InputTokens <= 10000000 && parsed.Usage.OutputTokens <= 10000000 && parsed.Usage.TotalTokens == parsed.Usage.InputTokens+parsed.Usage.OutputTokens {
		usage = parsed.Usage
		tokens = usage.TotalTokens
	} else {
		parsed.Usage = nil
	}
	// Validate ALL outputs before persisting any. Never fetch provider URLs.
	decoded := make([][]byte, len(parsed.Data))
	media := make([]string, len(parsed.Data))
	for i, item := range parsed.Data {
		if item.URL != "" || item.Base64 == "" || len(item.Base64) > base64.StdEncoding.EncodedLen(services.MaxGatewayImageArtifactBytes) {
			return nil, imageError(502, "image_result_invalid", "Image results must contain bounded base64 images, not URLs")
		}
		data, err := base64.StdEncoding.DecodeString(item.Base64)
		if err != nil || len(data) > services.MaxGatewayImageArtifactBytes {
			return nil, imageError(502, "image_result_invalid", "Image result encoding is invalid")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (format != "png" && format != "jpeg" && format != "webp") || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 32*1024*1024 {
			return nil, imageError(502, "image_result_invalid", "Image output format or dimensions exceed Gateway limits")
		}
		decoded[i], media[i] = data, "image/"+format
	}
	uncertain = false
	providerSucceeded = true
	providerLatency = int(time.Since(providerStarted).Milliseconds())
	charge = float64(len(parsed.Data)) * (*model.ImagePricePerImage)
	created = parsed.Created
	for i, data := range decoded {
		id, err := s.imageArtifacts.Save(ctx, owner, model.ID, requestID, media[i], data)
		if err != nil {
			return nil, imageError(503, "image_storage_failed", "Images were generated but result persistence failed; budget estimate retained; do not regenerate automatically")
		}
		parsed.Data[i].ArtifactID = id
		artifactIDs = append(artifactIDs, id)
	}
	if err := s.taskAccessChecker(attribution).CheckModelAccess(ctx, userID, model.ID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsed.TraceID = traceID
	parsed.Accounting = map[string]interface{}{"request_id": requestID, "price_basis": "admin_per_image_estimate", "estimated_cost": charge, "currency": services.GatewayBudgetCurrency, "artifact_retention_days": 7,
		"routing_policy": imageRoutingPolicy(req.model), "selected_model": model.DisplayName, "selected_model_id": model.ID}
	return &parsed, nil
}

func (s *service) LoadImageArtifact(ctx context.Context, owner services.ImageArtifactOwner, id string, groups []string) (*services.ImageArtifact, error) {
	if s.imageArtifacts == nil {
		return nil, services.ErrImageArtifactUnavailable
	}
	artifact, err := s.imageArtifacts.Load(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	model, err := repository.ForModelUser(s.modelRepo, owner.UserID).GetByID(artifact.ModelID)
	if err != nil || model == nil || !model.IsActive || !modelAllowedForGatewayKey(model, groups) {
		return nil, services.ErrImageArtifactUnavailable
	}
	checker := s.modelAccessChecker
	if checker == nil {
		checker, _ = s.modelRepo.(repository.ModelAccessChecker)
	}
	if checker == nil || checker.CheckModelAccess(ctx, owner.UserID, artifact.ModelID) != nil {
		return nil, services.ErrImageArtifactUnavailable
	}
	return artifact, nil
}

func imageFailureCode(err error) string {
	if err == nil {
		return ""
	}
	var operationErr *ImageExecutionError
	if errors.As(err, &operationErr) {
		return operationErr.Code
	}
	if errors.Is(err, models.ErrModelUnavailable) {
		return "model_unavailable"
	}
	if errors.Is(err, services.ErrGatewayKeyBudgetExceeded) {
		return "budget_exceeded"
	}
	if errors.Is(err, services.ErrGatewayDeploymentUnavailable) {
		return "image_deployment_unavailable"
	}
	if errors.Is(err, context.Canceled) {
		return "request_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "request_timeout"
	}
	return "image_governance_failed"
}
