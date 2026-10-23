package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

// Classification only proposes a quality tier. Authorization, security and
// deployment selection remain owned by the gateway, not the classifier.
type routingDecision struct {
	Policy                   string             `json:"policy"`
	Classifier               string             `json:"classifier"`
	RequestedClassifier      string             `json:"requested_classifier,omitempty"`
	ClassifierFallback       string             `json:"classifier_fallback,omitempty"`
	ClassifierConfidence     *float64           `json:"classifier_confidence,omitempty"`
	ClassifierConfidenceKind string             `json:"classifier_confidence_kind,omitempty"`
	RuleAssessment           *RuleAssessment    `json:"rule_assessment,omitempty"`
	ClassificationDurationMS float64            `json:"classification_duration_ms,omitempty"`
	Difficulty               string             `json:"difficulty"`
	Score                    float64            `json:"score"`
	RequiredTier             int                `json:"required_tier"`
	SelectedTier             int                `json:"selected_tier"`
	MinimumTier              int                `json:"minimum_candidate_tier"`
	Signals                  []string           `json:"signals"`
	Warnings                 []string           `json:"warnings"`
	ModelID                  int                `json:"model_id"`
	ModelGroup               string             `json:"model_group"`
	SecureRequired           bool               `json:"secure_required"`
	PromptTokens             int                `json:"estimated_prompt_tokens"`
	OutputTokens             int                `json:"estimated_output_tokens"`
	EstimatedCost            float64            `json:"estimated_cost"`
	Currency                 string             `json:"currency"`
	EligibleCandidates       int                `json:"eligible_candidates"`
	Rejections               []routingRejection `json:"rejections"`
	FailureReason            string             `json:"failure_reason,omitempty"`
}

type routingRejection struct {
	ModelID    int      `json:"model_id"`
	ModelGroup string   `json:"model_group"`
	Reasons    []string `json:"reasons"`
}

var errNoRoutingCandidate = errors.New("no eligible model for automatic routing")

func effectiveQualityTier(model *models.LLMModel) int {
	if model.QualityTier < 1 {
		return 1
	}
	return model.QualityTier
}

func routingModelGroup(model *models.LLMModel) string {
	if group := strings.TrimSpace(model.ModelGroup); group != "" {
		return group
	}
	return strings.TrimSpace(model.DisplayName)
}

func routingCurrency(model *models.LLMModel) string {
	return strings.ToUpper(defaultIfBlank(strings.TrimSpace(model.Currency), "USD"))
}

func modelTransportSupported(model *models.LLMModel, req ChatCompletionRequest) bool {
	if !models.LLMChatAvailable(model) {
		return false
	}
	if !visionTransportSupported(models.ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType), req) {
		return false
	}
	switch models.ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType) {
	case models.ProtocolTypeOpenAI, models.ProtocolTypeOpenAICompatible:
		return true
	case models.ProtocolTypeAnthropic:
		return req.N == nil || *req.N == 1
	default:
		return false
	}
}

func routingRejectionReasons(model *models.LLMModel, req ChatCompletionRequest, d routingDecision) []string {
	reasons := []string{}
	if model == nil {
		return []string{"missing_model"}
	}
	if !model.IsActive {
		reasons = append(reasons, "inactive")
	}
	if !models.LLMChatAvailable(model) {
		reasons = append(reasons, "not_a_chat_model")
	}
	if !modelAllowedForGatewayKey(model, req.AllowedModelGroups) {
		reasons = append(reasons, "key_not_allowed")
	}
	if !modelTransportSupported(model, req) {
		reasons = append(reasons, "unsupported_protocol_or_completion_count")
	}
	if requestRequiresTools(req) && modelRejectsTools(model) {
		reasons = append(reasons, "tools_not_supported")
	}
	if requestRequiresVision(req) && !models.LLMVisionSupported(model) {
		reasons = append(reasons, "vision_not_supported")
	}
	if req.BudgetController != nil && routingCurrency(model) != services.GatewayBudgetCurrency {
		reasons = append(reasons, "budget_currency_mismatch")
	}
	if d.SecureRequired && !model.IsSecure {
		reasons = append(reasons, "secure_model_required")
	}
	if effectiveQualityTier(model) < d.MinimumTier {
		reasons = append(reasons, "insufficient_quality_tier")
	}
	// A provider context window must accommodate both the input and the output
	// reservation. Checking only the prompt lets routing pick a deployment
	// which is guaranteed to fail once generation starts.
	if model.ContextWindowTokens > 0 && model.ContextWindowTokens < d.PromptTokens+d.OutputTokens {
		reasons = append(reasons, "context_capacity_exceeded")
	}
	return reasons
}

func requestRequiresTools(req ChatCompletionRequest) bool {
	// An empty tools array or JSON null is not a tool-using request.
	var tools []json.RawMessage
	if err := json.Unmarshal(req.Tools, &tools); err == nil {
		return len(tools) > 0
	}
	return len(strings.TrimSpace(string(req.Tools))) > 0
}

func modelRejectsTools(model *models.LLMModel) bool {
	if model.ToolsSupportOverride != nil {
		return !*model.ToolsSupportOverride
	}
	// Name-based classification and missing metadata cannot establish lack of
	// tool support. Preserve the previous provider-metadata contract only.
	return model.CapabilitySource == "provider_metadata" && !model.SupportsTools
}

func routingCandidateAllowed(model *models.LLMModel, req ChatCompletionRequest, d routingDecision) bool {
	return len(routingRejectionReasons(model, req, d)) == 0
}

func (s *service) selectContentModel(ctx context.Context, req ChatCompletionRequest, secureRequired bool) (*models.LLMModel, *routingDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, &routingDecision{}, err
	}
	if s.modelRepo == nil {
		return nil, &routingDecision{}, fmt.Errorf("model repository is not configured")
	}
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return nil, &routingDecision{}, fmt.Errorf("failed to list routing candidates: %w", err)
	}
	d, classificationErr := s.classifyRoutingTask(ctx, req.Messages, secureRequired)
	if classificationErr != nil {
		return nil, &d, classificationErr
	}
	d.SecureRequired = secureRequired
	d.PromptTokens, d.OutputTokens = routingTokenEstimate(req)
	eligible := []models.LLMModel{}
	highestTier := 0
	for _, item := range orderModelDeployments(items) {
		if reasons := routingRejectionReasons(&item, req, d); len(reasons) > 0 {
			d.Rejections = append(d.Rejections, routingRejection{item.ID, routingModelGroup(&item), reasons})
			continue
		}
		eligible = append(eligible, item)
		if tier := effectiveQualityTier(&item); tier > highestTier {
			highestTier = tier
		}
	}
	d.EligibleCandidates = len(eligible)
	if len(eligible) == 0 {
		d.FailureReason = errNoRoutingCandidate.Error()
		if requestRequiresVision(req) {
			d.FailureReason = models.ErrNoVisionModel.Error()
			return nil, &d, models.ErrNoVisionModel
		}
		return nil, &d, errNoRoutingCandidate
	}
	d.MinimumTier = d.RequiredTier
	if highestTier < d.RequiredTier {
		d.MinimumTier = highestTier
		d.Warnings = append(d.Warnings, "insufficient_quality_tier_best_available")
	}
	// Preserve deployment priority WITHIN each logical group. Cost routing is
	// between groups, not a replacement for an administrator's failover order.
	groups := map[string]bool{}
	candidates := []models.LLMModel{}
	for _, item := range eligible {
		if effectiveQualityTier(&item) < d.MinimumTier {
			d.Rejections = append(d.Rejections, routingRejection{item.ID, routingModelGroup(&item), []string{"insufficient_quality_tier"}})
			continue
		}
		group := strings.ToLower(routingModelGroup(&item))
		if groups[group] {
			continue
		}
		groups[group] = true
		candidates = append(candidates, item)
	}
	selected := candidates[0]
	comparable := true
	for _, item := range candidates {
		if routingCurrency(&item) != routingCurrency(&selected) {
			comparable = false
			break
		}
	}
	if comparable {
		for _, item := range candidates[1:] {
			if calculateEstimatedCost(&item, d.PromptTokens, d.OutputTokens) < calculateEstimatedCost(&selected, d.PromptTokens, d.OutputTokens) {
				selected = item
			}
		}
	} else {
		// No exchange-rate assumptions. Keep deterministic priority/name order.
		d.Warnings = append(d.Warnings, "mixed_currencies_priority_order_no_cost_comparison")
	}
	if selected.QualityTier == 0 {
		d.Warnings = append(d.Warnings, "unclassified_model_assumed_basic")
	}
	if selected.ContextWindowTokens == 0 {
		d.Warnings = append(d.Warnings, "context_capacity_unknown")
	}
	d.SelectedTier, d.ModelID, d.ModelGroup = effectiveQualityTier(&selected), selected.ID, routingModelGroup(&selected)
	d.EstimatedCost = calculateEstimatedCost(&selected, d.PromptTokens, d.OutputTokens)
	d.Currency = routingCurrency(&selected)
	return &selected, &d, nil
}

func (s *service) recordRoutingDecision(prepared *preparedChatRequest, decision *routingDecision) {
	if decision == nil {
		return
	}
	details := mustJSONString(decision)
	severity := models.AuditSeverityInfo
	eventType := "gateway.routing.selected"
	message := fmt.Sprintf("Auto routing: %s (tier %d) -> %s (tier %d)", decision.Difficulty, decision.RequiredTier, decision.ModelGroup, decision.SelectedTier)
	if decision.FailureReason != "" {
		eventType = "gateway.routing.rejected"
		message = "Auto routing rejected: " + decision.FailureReason
		severity = models.AuditSeverityWarn
	}
	if len(decision.Warnings) > 0 {
		severity = models.AuditSeverityWarn
	}
	if err := s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID: prepared.traceID, SessionID: prepared.sessionIDPtr, RequestID: prepared.requestIDPtr,
		UserID: prepared.userIDPtr, InstanceID: prepared.req.InstanceID, GatewayKeyID: prepared.req.GatewayKeyID,
		InstanceMode: runtimeAttributionString(prepared.req.InstanceMode), RuntimeType: runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID: runtimeAttributionString(prepared.req.GatewayID), RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
		EventType: eventType, TrafficClass: models.TrafficClassLLM, Severity: severity,
		Message: message,
		Details: &details,
	}); err != nil {
		logPersistenceError("record gateway.routing.selected", prepared.traceID, err)
	}
}
