package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

func (prepared *preparedChatRequest) reserveProviderAudit(ctx context.Context, providerRequestBody []byte) error {
	if !prepared.observabilityAdmitted {
		return nil
	}
	payload := requestAuditPayload(prepared.req, providerRequestBody)
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return repository.PreflightObservedRequest(ctx, prepared.userID, prepared.requestID, int64(len(serialized)))
}

func gatewayAuditMessage(message string) string {
	const maxSummaryCharacters = 500
	chars, ellipsisIndex := 0, 0
	for i := range message {
		if chars == maxSummaryCharacters-1 {
			ellipsisIndex = i
		}
		if chars == maxSummaryCharacters {
			return message[:ellipsisIndex] + "…"
		}
		chars++
	}
	return message
}

func (s *service) recordFailure(traceID, requestID string, req ChatCompletionRequest, userID int, model *models.LLMModel, startedAt time.Time, failure string, providerRequestBody []byte, attemptNumbers ...int) {
	completedAt := time.Now()
	latencyMs := int(time.Since(startedAt).Milliseconds())
	attemptNo := 1
	if len(attemptNumbers) > 0 && attemptNumbers[0] > 0 {
		attemptNo = attemptNumbers[0]
	}
	promptTokens := 0
	completionTokens := 0
	if len(attemptNumbers) > 1 && attemptNumbers[1] > 0 {
		promptTokens = attemptNumbers[1]
	}
	if len(attemptNumbers) > 2 && attemptNumbers[2] > 0 {
		completionTokens = attemptNumbers[2]
	}
	requestPayload := requestAuditPayload(req, providerRequestBody)
	responsePayload := failure
	summary := gatewayAuditMessage(failure)
	invocation := &models.ModelInvocation{
		TraceID:             traceID,
		SessionID:           req.SessionID,
		RequestID:           requestID,
		UserID:              intPtr(userID),
		InstanceID:          req.InstanceID,
		InstanceMode:        runtimeAttributionString(req.InstanceMode),
		RuntimeType:         runtimeAttributionString(req.RuntimeType),
		GatewayID:           runtimeAttributionString(req.GatewayID),
		RuntimePodID:        runtimeAttributionInt64(req.RuntimePodID),
		GatewayKeyID:        req.GatewayKeyID,
		ModelID:             intPtr(model.ID),
		ProviderType:        model.ProviderType,
		RequestedModel:      req.Model,
		ActualProviderModel: model.ProviderModelName,
		TrafficClass:        models.TrafficClassLLM,
		RequestPayload:      &requestPayload,
		ResponsePayload:     &responsePayload,
		PromptTokens:        promptTokens,
		CompletionTokens:    completionTokens,
		TotalTokens:         promptTokens + completionTokens,
		LatencyMs:           &latencyMs,
		AttemptNo:           attemptNo,
		IsStreaming:         req.Stream,
		Status:              models.ModelInvocationStatusFailed,
		ErrorMessage:        &summary,
		CompletedAt:         &completedAt,
	}
	if err := s.invocationService.RecordInvocation(invocation); err != nil {
		logPersistenceError("record failed invocation", traceID, err)
	}

	providerRequestPayload := requestPayload
	if err := s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID:      traceID,
		SessionID:    req.SessionID,
		RequestID:    stringPtr(requestID),
		UserID:       intPtr(userID),
		InstanceID:   req.InstanceID,
		InstanceMode: runtimeAttributionString(req.InstanceMode),
		RuntimeType:  runtimeAttributionString(req.RuntimeType),
		GatewayID:    runtimeAttributionString(req.GatewayID),
		RuntimePodID: runtimeAttributionInt64(req.RuntimePodID),
		GatewayKeyID: req.GatewayKeyID,
		InvocationID: intPtr(invocation.ID),
		EventType:    "gateway.request.failed",
		TrafficClass: models.TrafficClassLLM,
		Severity:     models.AuditSeverityError,
		Message:      summary,
		Details:      &providerRequestPayload,
	}); err != nil {
		logPersistenceError("record gateway.request.failed", traceID, err)
	}
}

func (s *service) recordSuccess(prepared *preparedChatRequest, providerRequestBody []byte, responsePayload, assistantContent string, promptTokens, completionTokens, totalTokens, latencyMs int, isStreaming bool) {
	completedAt := time.Now()
	requestPayload := requestAuditPayload(prepared.req, providerRequestBody)
	promptTokens, completionTokens, totalTokens, usageEstimated := resolveRequestUsage(prepared.req, assistantContent, promptTokens, completionTokens, totalTokens)
	prepared.attemptPromptTokens = promptTokens
	prepared.attemptCompletionTokens = completionTokens
	if prepared.attemptNumber <= 0 {
		prepared.attemptNumber = 1
	}
	prepared.attemptSucceeded = true
	if isStreaming && prepared.attemptGenerationMs <= 0 {
		prepared.attemptGenerationMs = latencyMs
		if prepared.attemptTTFTMs > 0 && latencyMs > prepared.attemptTTFTMs {
			prepared.attemptGenerationMs = latencyMs - prepared.attemptTTFTMs
		}
	}
	if isStreaming && completionTokens > 0 && prepared.attemptGenerationMs > 0 {
		prepared.attemptTokensPerSecond = float64(completionTokens) / (float64(prepared.attemptGenerationMs) / 1000.0)
	}
	var ttftMs *int
	if prepared.attemptTTFTMs > 0 {
		ttftMs = intPtr(prepared.attemptTTFTMs)
	}
	var generationMs *int
	if prepared.attemptGenerationMs > 0 {
		generationMs = intPtr(prepared.attemptGenerationMs)
	}
	var tokensPerSecond *float64
	if prepared.attemptTokensPerSecond > 0 {
		value := prepared.attemptTokensPerSecond
		tokensPerSecond = &value
	}
	if prepared.req.UsageFinalizer != nil {
		if err := prepared.req.UsageFinalizer(prepared.priorAttemptTokens + totalTokens); err != nil {
			logPersistenceError("finalize gateway key token usage", prepared.traceID, err)
		}
	}
	invocation := &models.ModelInvocation{
		TraceID:             prepared.traceID,
		SessionID:           prepared.sessionIDPtr,
		RequestID:           prepared.requestID,
		UserID:              prepared.userIDPtr,
		InstanceID:          prepared.req.InstanceID,
		InstanceMode:        runtimeAttributionString(prepared.req.InstanceMode),
		RuntimeType:         runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID:           runtimeAttributionString(prepared.req.GatewayID),
		RuntimePodID:        runtimeAttributionInt64(prepared.req.RuntimePodID),
		GatewayKeyID:        prepared.req.GatewayKeyID,
		ModelID:             intPtr(prepared.resolvedModel.ID),
		ProviderType:        prepared.resolvedModel.ProviderType,
		RequestedModel:      prepared.req.Model,
		ActualProviderModel: prepared.resolvedModel.ProviderModelName,
		TrafficClass:        models.TrafficClassLLM,
		RequestPayload:      &requestPayload,
		ResponsePayload:     &responsePayload,
		PromptTokens:        promptTokens,
		CompletionTokens:    completionTokens,
		TotalTokens:         totalTokens,
		LatencyMs:           &latencyMs,
		TTFTMs:              ttftMs,
		GenerationMs:        generationMs,
		TokensPerSecond:     tokensPerSecond,
		AttemptNo:           prepared.attemptNumber,
		IsStreaming:         isStreaming,
		Status:              models.ModelInvocationStatusCompleted,
		CompletedAt:         &completedAt,
	}
	if err := s.invocationService.RecordInvocation(invocation); err != nil {
		logPersistenceError("record completed invocation", prepared.traceID, err)
	}
	if err := s.chatMessageService.RecordMessages(
		prepared.traceID,
		prepared.sessionID,
		prepared.requestIDPtr,
		prepared.userIDPtr,
		prepared.req.InstanceID,
		intPtr(invocation.ID),
		[]services.PersistedChatMessage{
			{
				Role:       "assistant",
				Content:    assistantContent,
				SequenceNo: len(prepared.req.Messages) + 1,
			},
		},
	); err != nil {
		logPersistenceError("record assistant messages", prepared.traceID, err)
	}

	estimatedCost := calculateEstimatedCost(prepared.resolvedModel, promptTokens, completionTokens)
	settledCost := prepared.priorAttemptCost + estimatedCost
	if prepared.req.BudgetController != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := prepared.req.BudgetController.Finalize(ctx, settledCost, routingCurrency(prepared.resolvedModel)); err != nil {
			logPersistenceError("finalize gateway key budget", prepared.traceID, err)
		}
		cancel()
	}
	internalCost := 0.0
	if prepared.resolvedModel.IsSecure {
		internalCost = estimatedCost
	}
	if err := s.costRecordService.RecordCost(&models.CostRecord{
		EventUID:           stringPtr(repository.ObservationAttemptUID("cost_records", prepared.userID, prepared.requestID, prepared.attemptNumber, prepared.resolvedModel.ProviderType, prepared.resolvedModel.ProviderModelName)),
		InvocationEventUID: invocation.EventUID,
		TraceID:            prepared.traceID,
		SessionID:          prepared.req.SessionID,
		RequestID:          prepared.requestIDPtr,
		UserID:             prepared.userIDPtr,
		InstanceID:         prepared.req.InstanceID,
		InstanceMode:       runtimeAttributionString(prepared.req.InstanceMode),
		RuntimeType:        runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID:          runtimeAttributionString(prepared.req.GatewayID),
		RuntimePodID:       runtimeAttributionInt64(prepared.req.RuntimePodID),
		GatewayKeyID:       prepared.req.GatewayKeyID,
		InvocationID:       intPtr(invocation.ID),
		ModelID:            intPtr(prepared.resolvedModel.ID),
		ProviderType:       prepared.resolvedModel.ProviderType,
		ModelName:          prepared.resolvedModel.DisplayName,
		Currency:           fallbackCurrency(prepared.resolvedModel.Currency),
		PromptTokens:       promptTokens,
		CompletionTokens:   completionTokens,
		TotalTokens:        totalTokens,
		InputUnitPrice:     prepared.resolvedModel.InputPrice,
		OutputUnitPrice:    prepared.resolvedModel.OutputPrice,
		EstimatedCost:      estimatedCost,
		InternalCost:       internalCost,
	}); err != nil {
		logPersistenceError("record cost", prepared.traceID, err)
	}

	if err := s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID:      prepared.traceID,
		SessionID:    prepared.sessionIDPtr,
		RequestID:    prepared.requestIDPtr,
		UserID:       prepared.userIDPtr,
		InstanceID:   prepared.req.InstanceID,
		InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
		RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
		RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
		GatewayKeyID: prepared.req.GatewayKeyID,
		InvocationID: intPtr(invocation.ID),
		EventType:    "gateway.request.completed",
		TrafficClass: models.TrafficClassLLM,
		Severity:     models.AuditSeverityInfo,
		Message:      fmt.Sprintf("Completed LLM request for model %s", prepared.resolvedModel.DisplayName),
	}); err != nil {
		logPersistenceError("record gateway.request.completed", prepared.traceID, err)
	}
	if usageEstimated {
		if err := s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.sessionIDPtr,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
			RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
			GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
			RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
			GatewayKeyID: prepared.req.GatewayKeyID,
			InvocationID: intPtr(invocation.ID),
			EventType:    "gateway.usage.estimated",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      "Provider usage was missing; token usage was estimated locally.",
		}); err != nil {
			logPersistenceError("record gateway.usage.estimated", prepared.traceID, err)
		}
	}
}

func requestAuditPayload(req ChatCompletionRequest, providerRequestBody []byte) string {
	if payload := visionSafePayload(providerRequestBody); strings.TrimSpace(payload) != "" {
		return payload
	}
	return rawOrJSONRequestPayload(req)
}

func rawOrJSONRequestPayload(req ChatCompletionRequest) string {
	if len(req.RawBody) > 0 {
		return visionSafePayload(req.RawBody)
	}
	return visionSafePayload([]byte(mustJSONString(req)))
}
