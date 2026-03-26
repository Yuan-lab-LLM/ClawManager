package aigateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

// ToolCallFunction represents a tool/function call payload.
type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCall represents a tool call emitted by an assistant response.
type ToolCall struct {
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function *ToolCallFunction `json:"function,omitempty"`
	Index    *int              `json:"index,omitempty"`
}

// ChatMessage represents an OpenAI-compatible chat message.
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Refusal    interface{} `json:"refusal,omitempty"`
	Audio      interface{} `json:"audio,omitempty"`
}

// ChatCompletionRequest is the platform gateway request shape.
type ChatCompletionRequest struct {
	RawBody           []byte          `json:"-"`
	Model             string          `json:"model"`
	Messages          []ChatMessage   `json:"messages"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	MaxTokens         *int            `json:"max_tokens,omitempty"`
	Stream            bool            `json:"stream"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	ResponseFormat    json.RawMessage `json:"response_format,omitempty"`
	Stop              json.RawMessage `json:"stop,omitempty"`
	N                 *int            `json:"n,omitempty"`
	FrequencyPenalty  *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty   *float64        `json:"presence_penalty,omitempty"`
	ReasoningEffort   *string         `json:"reasoning_effort,omitempty"`
	StreamOptions     json.RawMessage `json:"stream_options,omitempty"`
	User              *string         `json:"user,omitempty"`
	SessionID         *string         `json:"session_id,omitempty"`
	InstanceID        *int            `json:"instance_id,omitempty"`
	TraceID           *string         `json:"trace_id,omitempty"`
	RequestID         *string         `json:"request_id,omitempty"`
}

// ChatCompletionResponse is used for audit parsing only.
type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string      `json:"role"`
			Content   interface{} `json:"content"`
			ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
			Refusal   interface{} `json:"refusal,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ProxyResponse is the raw provider response returned to the client.
type ProxyResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// AvailableModel represents a user-selectable active model.
type AvailableModel struct {
	ID          int     `json:"id"`
	DisplayName string  `json:"display_name"`
	Description *string `json:"description,omitempty"`
	IsSecure    bool    `json:"is_secure"`
	Provider    string  `json:"provider_type"`
}

const autoModelID = "auto"
const maxStoredIdentifierLength = 100

type preparedChatRequest struct {
	traceID       string
	sessionID     string
	sessionIDPtr  *string
	requestID     string
	requestIDPtr  *string
	userID        int
	userIDPtr     *int
	selectedModel *models.LLMModel
	resolvedModel *models.LLMModel
	req           ChatCompletionRequest
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	Model   string `json:"model,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string      `json:"role,omitempty"`
			Content   interface{} `json:"content,omitempty"`
			ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
			Refusal   interface{} `json:"refusal,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// Service defines gateway operations.
type Service interface {
	ListAvailableModels() ([]AvailableModel, error)
	ChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest) (*ProxyResponse, string, error)
	StreamChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest, w http.ResponseWriter) (string, error)
}

type service struct {
	modelRepo          repository.LLMModelRepository
	invocationService  services.ModelInvocationService
	auditEventService  services.AuditEventService
	costRecordService  services.CostRecordService
	riskDetector       services.RiskDetectionService
	riskHitService     services.RiskHitService
	chatSessionService services.ChatSessionService
	chatMessageService services.ChatMessageService
	secretRefService   services.SecretRefService
	httpClient         *http.Client
}

// NewService creates a new AI gateway service.
func NewService(
	modelRepo repository.LLMModelRepository,
	invocationService services.ModelInvocationService,
	auditEventService services.AuditEventService,
	costRecordService services.CostRecordService,
	riskDetector services.RiskDetectionService,
	riskHitService services.RiskHitService,
	chatSessionService services.ChatSessionService,
	chatMessageService services.ChatMessageService,
) Service {
	return &service{
		modelRepo:          modelRepo,
		invocationService:  invocationService,
		auditEventService:  auditEventService,
		costRecordService:  costRecordService,
		riskDetector:       riskDetector,
		riskHitService:     riskHitService,
		chatSessionService: chatSessionService,
		chatMessageService: chatMessageService,
		secretRefService:   services.NewSecretRefService(),
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (s *service) ListAvailableModels() ([]AvailableModel, error) {
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("failed to list available models: %w", err)
	}
	if len(items) == 0 {
		return []AvailableModel{}, nil
	}

	return []AvailableModel{
		{
			ID:          0,
			DisplayName: "Auto",
			Description: stringPtr("Automatically route requests to the best available model under current governance policy."),
			IsSecure:    false,
			Provider:    "gateway",
		},
	}, nil
}

func (s *service) ChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest) (*ProxyResponse, string, error) {
	prepared, err := s.prepareChatRequest(userID, req)
	if err != nil {
		traceID := ""
		if prepared != nil {
			traceID = prepared.traceID
		}
		return nil, traceID, err
	}

	switch prepared.resolvedModel.ProviderType {
	case "openai", "openai-compatible", "local":
		return s.callOpenAICompatible(ctx, prepared)
	default:
		_ = s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.req.SessionID,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			EventType:    "gateway.request.blocked",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      fmt.Sprintf("Provider type %s is not supported yet", prepared.resolvedModel.ProviderType),
		})
		return nil, prepared.traceID, errors.New("provider type is not supported yet")
	}
}

func (s *service) StreamChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest, w http.ResponseWriter) (string, error) {
	prepared, err := s.prepareChatRequest(userID, req)
	if err != nil {
		traceID := ""
		if prepared != nil {
			traceID = prepared.traceID
		}
		return traceID, err
	}

	switch prepared.resolvedModel.ProviderType {
	case "openai", "openai-compatible", "local":
		return prepared.traceID, s.streamOpenAICompatible(ctx, prepared, w)
	default:
		_ = s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.req.SessionID,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			EventType:    "gateway.request.blocked",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      fmt.Sprintf("Provider type %s is not supported yet", prepared.resolvedModel.ProviderType),
		})
		return prepared.traceID, errors.New("provider type is not supported yet")
	}
}

func (s *service) prepareChatRequest(userID int, req ChatCompletionRequest) (*preparedChatRequest, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("model is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("messages are required")
	}

	requestedModel := strings.TrimSpace(req.Model)
	selectedModel, err := s.resolveRequestedModel(requestedModel)
	if err != nil {
		return nil, err
	}

	prepared := &preparedChatRequest{
		userID:        userID,
		userIDPtr:     intPtr(userID),
		selectedModel: selectedModel,
		req:           req,
	}
	prepared.sessionID = resolveSessionID(req)
	prepared.traceID = s.resolveTraceID(userID, req, prepared.sessionID)
	if prepared.sessionID == "" {
		prepared.sessionID = normalizeSessionID(nil, prepared.traceID)
	}
	prepared.sessionIDPtr = stringPtr(prepared.sessionID)
	prepared.req.SessionID = prepared.sessionIDPtr
	prepared.requestID = normalizeOrCreateID(req.RequestID, "req")
	prepared.requestIDPtr = stringPtr(prepared.requestID)

	sessionTitle := deriveSessionTitle(prepared.req.Messages)
	if _, err := s.chatSessionService.EnsureSession(prepared.sessionID, prepared.userIDPtr, prepared.req.InstanceID, stringPtr(prepared.traceID), sessionTitle); err != nil {
		logPersistenceError("ensure chat session", prepared.traceID, err)
	}
	if err := s.chatMessageService.RecordMessages(
		prepared.traceID,
		prepared.sessionID,
		prepared.requestIDPtr,
		prepared.userIDPtr,
		prepared.req.InstanceID,
		nil,
		buildPersistedMessages(prepared.req.Messages),
	); err != nil {
		logPersistenceError("record request messages", prepared.traceID, err)
	}

	if err := s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID:      prepared.traceID,
		SessionID:    prepared.req.SessionID,
		RequestID:    prepared.requestIDPtr,
		UserID:       prepared.userIDPtr,
		InstanceID:   prepared.req.InstanceID,
		EventType:    "gateway.request.received",
		TrafficClass: models.TrafficClassLLM,
		Severity:     models.AuditSeverityInfo,
		Message:      fmt.Sprintf("Received LLM request for model %s", prepared.req.Model),
	}); err != nil {
		logPersistenceError("record gateway.request.received", prepared.traceID, err)
	}

	riskAnalysis := s.riskDetector.AnalyzeText(flattenMessages(prepared.req.Messages))
	if riskAnalysis.IsSensitive {
		if err := s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.req.SessionID,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			EventType:    "gateway.risk.detected",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      fmt.Sprintf("Sensitive content detected with %d hit(s)", len(riskAnalysis.Hits)),
		}); err != nil {
			logPersistenceError("record gateway.risk.detected", prepared.traceID, err)
		}
	}

	resolvedModel, riskAction, resolveErr := s.resolveTargetModel(prepared.selectedModel, riskAnalysis)
	if resolveErr != nil {
		blockedInvocationID := s.recordBlockedInvocation(prepared.traceID, prepared.requestID, prepared.req, prepared.userID, prepared.selectedModel, resolveErr.Error())
		if err := s.riskHitService.RecordHits(prepared.traceID, prepared.req.SessionID, prepared.requestIDPtr, prepared.userIDPtr, prepared.req.InstanceID, blockedInvocationID, riskAction, riskAnalysis.Hits); err != nil {
			logPersistenceError("record blocked risk hits", prepared.traceID, err)
		}
		if err := s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.req.SessionID,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			InvocationID: blockedInvocationID,
			EventType:    "gateway.request.blocked",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      resolveErr.Error(),
		}); err != nil {
			logPersistenceError("record gateway.request.blocked", prepared.traceID, err)
		}
		return prepared, resolveErr
	}

	if riskAnalysis.IsSensitive {
		if err := s.riskHitService.RecordHits(prepared.traceID, prepared.req.SessionID, prepared.requestIDPtr, prepared.userIDPtr, prepared.req.InstanceID, nil, riskAction, riskAnalysis.Hits); err != nil {
			logPersistenceError("record risk hits", prepared.traceID, err)
		}
		if riskAction == models.RiskActionRouteSecureModel && resolvedModel != nil && resolvedModel.ID != prepared.selectedModel.ID {
			if err := s.auditEventService.RecordEvent(&models.AuditEvent{
				TraceID:      prepared.traceID,
				SessionID:    prepared.req.SessionID,
				RequestID:    prepared.requestIDPtr,
				UserID:       prepared.userIDPtr,
				InstanceID:   prepared.req.InstanceID,
				EventType:    "gateway.request.rerouted",
				TrafficClass: models.TrafficClassLLM,
				Severity:     models.AuditSeverityWarn,
				Message:      fmt.Sprintf("Sensitive content rerouted from model %s to secure model %s", prepared.selectedModel.DisplayName, resolvedModel.DisplayName),
			}); err != nil {
				logPersistenceError("record gateway.request.rerouted", prepared.traceID, err)
			}
		}
	}

	prepared.resolvedModel = resolvedModel
	return prepared, nil
}

func (s *service) callOpenAICompatible(ctx context.Context, prepared *preparedChatRequest) (*ProxyResponse, string, error) {
	resolvedAPIKey, err := s.secretRefService.ResolveString(ctx, prepared.resolvedModel.APIKey, prepared.resolvedModel.APIKeySecretRef)
	if err != nil {
		return nil, prepared.traceID, err
	}

	providerRequestBody, err := buildProviderRequestBody(prepared.req, prepared.resolvedModel)
	if err != nil {
		return nil, prepared.traceID, err
	}

	httpRequest, err := buildProviderHTTPRequest(ctx, prepared.traceID, prepared.requestID, prepared.resolvedModel, providerRequestBody, resolvedAPIKey, false)
	if err != nil {
		return nil, prepared.traceID, err
	}

	startedAt := time.Now()
	response, err := s.httpClient.Do(httpRequest)
	if err != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("provider call failed: %v", err), providerRequestBody)
		return nil, prepared.traceID, fmt.Errorf("failed to call provider: %w", err)
	}
	defer response.Body.Close()

	responseBody, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("failed to read provider response: %v", readErr), providerRequestBody)
		return nil, prepared.traceID, fmt.Errorf("failed to read provider response: %w", readErr)
	}

	proxyResponse := &ProxyResponse{
		StatusCode: response.StatusCode,
		Headers:    cloneProxyHeaders(response.Header),
		Body:       responseBody,
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, "provider returned non-success status: "+message, providerRequestBody)
		return proxyResponse, prepared.traceID, nil
	}

	var providerResponse ChatCompletionResponse
	if err := json.Unmarshal(responseBody, &providerResponse); err != nil {
		s.recordSuccess(prepared, providerRequestBody, string(responseBody), "", 0, 0, 0, int(time.Since(startedAt).Milliseconds()), false)
		return proxyResponse, prepared.traceID, nil
	}

	assistantContent := extractAssistantContent(providerResponse)
	s.recordSuccess(prepared, providerRequestBody, string(responseBody), assistantContent, providerResponse.Usage.PromptTokens, providerResponse.Usage.CompletionTokens, providerResponse.Usage.TotalTokens, int(time.Since(startedAt).Milliseconds()), false)
	return proxyResponse, prepared.traceID, nil
}

func (s *service) recordFailure(traceID, requestID string, req ChatCompletionRequest, userID int, model *models.LLMModel, startedAt time.Time, failure string, providerRequestBody []byte) {
	completedAt := time.Now()
	latencyMs := int(time.Since(startedAt).Milliseconds())
	requestPayload := string(providerRequestBody)
	if strings.TrimSpace(requestPayload) == "" {
		requestPayload = rawOrJSONRequestPayload(req)
	}
	responsePayload := failure
	invocation := &models.ModelInvocation{
		TraceID:             traceID,
		SessionID:           req.SessionID,
		RequestID:           requestID,
		UserID:              intPtr(userID),
		InstanceID:          req.InstanceID,
		ModelID:             intPtr(model.ID),
		ProviderType:        model.ProviderType,
		RequestedModel:      req.Model,
		ActualProviderModel: model.ProviderModelName,
		TrafficClass:        models.TrafficClassLLM,
		RequestPayload:      &requestPayload,
		ResponsePayload:     &responsePayload,
		LatencyMs:           &latencyMs,
		IsStreaming:         req.Stream,
		Status:              models.ModelInvocationStatusFailed,
		ErrorMessage:        stringPtr(failure),
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
		InvocationID: intPtr(invocation.ID),
		EventType:    "gateway.request.failed",
		TrafficClass: models.TrafficClassLLM,
		Severity:     models.AuditSeverityError,
		Message:      failure,
		Details:      &providerRequestPayload,
	}); err != nil {
		logPersistenceError("record gateway.request.failed", traceID, err)
	}
}

func (s *service) recordSuccess(prepared *preparedChatRequest, providerRequestBody []byte, responsePayload, assistantContent string, promptTokens, completionTokens, totalTokens, latencyMs int, isStreaming bool) {
	completedAt := time.Now()
	requestPayload := string(providerRequestBody)
	if strings.TrimSpace(requestPayload) == "" {
		requestPayload = rawOrJSONRequestPayload(prepared.req)
	}
	promptTokens, completionTokens, totalTokens, usageEstimated := resolveUsage(prepared.req.Messages, assistantContent, promptTokens, completionTokens, totalTokens)
	invocation := &models.ModelInvocation{
		TraceID:             prepared.traceID,
		SessionID:           prepared.sessionIDPtr,
		RequestID:           prepared.requestID,
		UserID:              prepared.userIDPtr,
		InstanceID:          prepared.req.InstanceID,
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
	internalCost := 0.0
	if prepared.resolvedModel.IsSecure {
		internalCost = estimatedCost
	}
	if err := s.costRecordService.RecordCost(&models.CostRecord{
		TraceID:          prepared.traceID,
		SessionID:        prepared.req.SessionID,
		RequestID:        prepared.requestIDPtr,
		UserID:           prepared.userIDPtr,
		InstanceID:       prepared.req.InstanceID,
		InvocationID:     intPtr(invocation.ID),
		ModelID:          intPtr(prepared.resolvedModel.ID),
		ProviderType:     prepared.resolvedModel.ProviderType,
		ModelName:        prepared.resolvedModel.DisplayName,
		Currency:         fallbackCurrency(prepared.resolvedModel.Currency),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		InputUnitPrice:   prepared.resolvedModel.InputPrice,
		OutputUnitPrice:  prepared.resolvedModel.OutputPrice,
		EstimatedCost:    estimatedCost,
		InternalCost:     internalCost,
	}); err != nil {
		logPersistenceError("record cost", prepared.traceID, err)
	}

	if err := s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID:      prepared.traceID,
		SessionID:    prepared.sessionIDPtr,
		RequestID:    prepared.requestIDPtr,
		UserID:       prepared.userIDPtr,
		InstanceID:   prepared.req.InstanceID,
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

func (s *service) streamOpenAICompatible(ctx context.Context, prepared *preparedChatRequest, w http.ResponseWriter) error {
	resolvedAPIKey, err := s.secretRefService.ResolveString(ctx, prepared.resolvedModel.APIKey, prepared.resolvedModel.APIKeySecretRef)
	if err != nil {
		return err
	}

	providerRequestBody, err := buildProviderRequestBody(prepared.req, prepared.resolvedModel)
	if err != nil {
		return err
	}

	httpRequest, err := buildProviderHTTPRequest(ctx, prepared.traceID, prepared.requestID, prepared.resolvedModel, providerRequestBody, resolvedAPIKey, true)
	if err != nil {
		return err
	}

	startedAt := time.Now()
	response, err := s.httpClient.Do(httpRequest)
	if err != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("provider call failed: %v", err), providerRequestBody)
		return fmt.Errorf("failed to call provider: %w", err)
	}
	defer response.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming response writer is not supported")
	}

	copyProxyHeaders(w.Header(), response.Header)
	w.Header().Set("X-Trace-ID", prepared.traceID)
	w.WriteHeader(response.StatusCode)
	flusher.Flush()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(response.Body)
		if len(responseBody) > 0 {
			_, _ = w.Write(responseBody)
			flusher.Flush()
		}
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, "provider returned non-success status: "+message, providerRequestBody)
		return nil
	}

	reader := bufio.NewReader(response.Body)
	var rawStream strings.Builder
	var assistantText strings.Builder
	promptTokens := 0
	completionTokens := 0
	totalTokens := 0
	streamFailed := false

	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			done := inspectStreamLine(line, &assistantText, &promptTokens, &completionTokens, &totalTokens)
			rawStream.WriteString(line)
			if _, err := io.WriteString(w, line); err == nil {
				flusher.Flush()
			}
			if done {
				break
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			streamFailed = true
			s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("failed while reading provider stream: %v", readErr), providerRequestBody)
			break
		}
	}

	if !streamFailed {
		assistantContent := assistantText.String()
		if strings.TrimSpace(assistantContent) == "" {
			assistantContent = rawStream.String()
		}
		s.recordSuccess(prepared, providerRequestBody, rawStream.String(), assistantContent, promptTokens, completionTokens, totalTokens, int(time.Since(startedAt).Milliseconds()), true)
	}
	return nil
}

func inspectStreamLine(line string, assistantText *strings.Builder, promptTokens, completionTokens, totalTokens *int) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return false
	}

	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" {
		return false
	}
	if payload == "[DONE]" {
		return true
	}

	var chunk openAIStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return false
	}

	if chunk.Usage != nil {
		*promptTokens = chunk.Usage.PromptTokens
		*completionTokens = chunk.Usage.CompletionTokens
		*totalTokens = chunk.Usage.TotalTokens
	}

	for _, choice := range chunk.Choices {
		content := flattenMessageContent(choice.Delta.Content)
		if content != "" {
			assistantText.WriteString(content)
		}
	}
	return false
}

func buildProviderRequestBody(req ChatCompletionRequest, model *models.LLMModel) ([]byte, error) {
	if model == nil {
		return nil, errors.New("model is not active or does not exist")
	}
	payload := map[string]json.RawMessage{}
	if len(req.RawBody) > 0 {
		if err := json.Unmarshal(req.RawBody, &payload); err != nil {
			return nil, fmt.Errorf("failed to decode provider request: %w", err)
		}
	}
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}
	delete(payload, "session_id")
	delete(payload, "instance_id")
	delete(payload, "trace_id")
	delete(payload, "request_id")
	modelPayload, err := json.Marshal(model.ProviderModelName)
	if err != nil {
		return nil, fmt.Errorf("failed to encode provider model name: %w", err)
	}
	payload["model"] = json.RawMessage(modelPayload)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode provider request: %w", err)
	}
	return body, nil
}

func buildProviderHTTPRequest(ctx context.Context, traceID, requestID string, model *models.LLMModel, providerRequestBody []byte, resolvedAPIKey *string, acceptStream bool) (*http.Request, error) {

	endpoint := strings.TrimRight(strings.TrimSpace(model.BaseURL), "/") + "/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(providerRequestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to build provider request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if acceptStream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	} else {
		httpRequest.Header.Set("Accept", "application/json")
	}
	httpRequest.Header.Set("X-Trace-ID", traceID)
	httpRequest.Header.Set("X-Request-ID", requestID)
	if resolvedAPIKey != nil && strings.TrimSpace(*resolvedAPIKey) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+strings.TrimSpace(*resolvedAPIKey))
	}
	return httpRequest, nil
}

func (s *service) recordBlockedInvocation(traceID, requestID string, req ChatCompletionRequest, userID int, model *models.LLMModel, reason string) *int {
	requestPayload := mustJSONString(req)
	responsePayload := reason
	invocation := &models.ModelInvocation{
		TraceID:             traceID,
		SessionID:           req.SessionID,
		RequestID:           requestID,
		UserID:              intPtr(userID),
		InstanceID:          req.InstanceID,
		ModelID:             intPtr(model.ID),
		ProviderType:        model.ProviderType,
		RequestedModel:      req.Model,
		ActualProviderModel: model.ProviderModelName,
		TrafficClass:        models.TrafficClassLLM,
		RequestPayload:      &requestPayload,
		ResponsePayload:     &responsePayload,
		IsStreaming:         false,
		Status:              models.ModelInvocationStatusBlocked,
		ErrorMessage:        stringPtr(reason),
	}
	if err := s.invocationService.RecordInvocation(invocation); err != nil {
		return nil
	}
	return intPtr(invocation.ID)
}

func (s *service) resolveTargetModel(selectedModel *models.LLMModel, analysis services.RiskAnalysis) (*models.LLMModel, string, error) {
	if selectedModel == nil {
		return nil, models.RiskActionAllow, errors.New("model is not active or does not exist")
	}
	if !analysis.IsSensitive {
		return selectedModel, models.RiskActionAllow, nil
	}
	if analysis.HighestAction == "require_approval" || analysis.HighestAction == models.RiskActionBlock {
		return nil, models.RiskActionBlock, errors.New("request was blocked by risk policy")
	}
	if selectedModel.IsSecure {
		return selectedModel, models.RiskActionAllow, nil
	}

	activeModels, err := s.modelRepo.ListActive()
	if err != nil {
		return nil, models.RiskActionBlock, fmt.Errorf("failed to list active secure models: %w", err)
	}
	for _, item := range activeModels {
		if item.IsSecure {
			resolved := item
			return &resolved, models.RiskActionRouteSecureModel, nil
		}
	}
	return nil, models.RiskActionBlock, errors.New("sensitive content requires an active secure model")
}

func (s *service) resolveRequestedModel(requestedModel string) (*models.LLMModel, error) {
	if isAutoModelRequest(requestedModel) {
		return s.selectAutoModel()
	}

	selectedModel, err := s.modelRepo.GetByDisplayName(requestedModel)
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}
	if selectedModel == nil || !selectedModel.IsActive {
		return nil, errors.New("model is not active or does not exist")
	}
	return selectedModel, nil
}

func (s *service) selectAutoModel() (*models.LLMModel, error) {
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	if len(items) == 0 {
		return nil, errors.New("no active models are configured")
	}

	for _, item := range items {
		if !item.IsSecure {
			selected := item
			return &selected, nil
		}
	}

	selected := items[0]
	return &selected, nil
}

func isAutoModelRequest(requestedModel string) bool {
	return strings.EqualFold(strings.TrimSpace(requestedModel), autoModelID)
}

func calculateEstimatedCost(model *models.LLMModel, promptTokens, completionTokens int) float64 {
	return (float64(promptTokens) * model.InputPrice / 1_000_000.0) + (float64(completionTokens) * model.OutputPrice / 1_000_000.0)
}

func normalizeOrCreateID(value *string, prefix string) string {
	if value != nil {
		if normalized := normalizeExistingIdentifier(*value, prefix); normalized != "" {
			return normalized
		}
	}
	return prefix + "_" + randomHex(8)
}

func normalizeExistingIdentifier(rawValue string, prefix string) string {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= maxStoredIdentifierLength {
		return trimmed
	}

	sum := sha256.Sum256([]byte(trimmed))
	normalized := prefix + "_" + hex.EncodeToString(sum[:16])
	if len(normalized) > maxStoredIdentifierLength {
		return normalized[:maxStoredIdentifierLength]
	}
	return normalized
}

func (s *service) resolveTraceID(userID int, req ChatCompletionRequest, sessionID string) string {
	if req.TraceID != nil {
		if normalized := normalizeExistingIdentifier(*req.TraceID, "trc"); normalized != "" {
			return normalized
		}
	}

	toolReferenceIDs := extractToolReferenceIDs(req.Messages)
	if len(toolReferenceIDs) == 0 {
		return normalizeOrCreateID(nil, "trc")
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID != "" {
		if session, err := s.chatSessionService.GetSession(normalizedSessionID); err == nil && session != nil && session.LastTraceID != nil {
			lastTraceID := normalizeExistingIdentifier(*session.LastTraceID, "trc")
			if lastTraceID != "" {
				if instanceIDsCompatible(req.InstanceID, session.InstanceID) {
					return lastTraceID
				}
				items, listErr := s.invocationService.ListInvocationsByTraceID(lastTraceID)
				if listErr != nil {
					logPersistenceError("load session last trace invocations", lastTraceID, listErr)
				} else if invocationsContainAnyToolReference(items, toolReferenceIDs) {
					return lastTraceID
				}
			}
		} else if err != nil {
			logPersistenceError("load chat session for trace reuse", normalizedSessionID, err)
		}

		items, err := s.invocationService.ListInvocationsBySessionID(normalizedSessionID, 100)
		if err != nil {
			logPersistenceError("list invocations by session for trace reuse", normalizedSessionID, err)
		} else {
			for _, invocation := range items {
				if strings.TrimSpace(invocation.TraceID) == "" || invocation.ResponsePayload == nil {
					continue
				}
				if req.InstanceID != nil && invocation.InstanceID != nil && *req.InstanceID != *invocation.InstanceID {
					continue
				}
				if payloadContainsAny(*invocation.ResponsePayload, toolReferenceIDs) {
					return normalizeExistingIdentifier(invocation.TraceID, "trc")
				}
			}
		}
	}

	items, err := s.invocationService.ListInvocationsByUserID(userID, 200)
	if err != nil {
		logPersistenceError("list invocations for trace reuse", "", err)
		return normalizeOrCreateID(nil, "trc")
	}

	for _, invocation := range items {
		if strings.TrimSpace(invocation.TraceID) == "" || invocation.ResponsePayload == nil {
			continue
		}
		if req.InstanceID != nil && invocation.InstanceID != nil && *req.InstanceID != *invocation.InstanceID {
			continue
		}
		if payloadContainsAny(*invocation.ResponsePayload, toolReferenceIDs) {
			return normalizeExistingIdentifier(invocation.TraceID, "trc")
		}
	}

	return normalizeOrCreateID(nil, "trc")
}

func randomHex(size int) string {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func intPtr(value int) *int {
	return &value
}

func userIDOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func instanceIDsCompatible(requestInstanceID, sessionInstanceID *int) bool {
	if requestInstanceID == nil || sessionInstanceID == nil {
		return true
	}
	return *requestInstanceID == *sessionInstanceID
}

func stringPtr(value string) *string {
	return &value
}

func mustJSONString(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func rawOrJSONRequestPayload(req ChatCompletionRequest) string {
	if len(req.RawBody) > 0 {
		return string(req.RawBody)
	}
	return mustJSONString(req)
}

func cloneProxyHeaders(headers http.Header) http.Header {
	cloned := make(http.Header)
	copyProxyHeaders(cloned, headers)
	return cloned
}

func copyProxyHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) || strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopByHopHeader(header string) bool {
	switch strings.ToLower(strings.TrimSpace(header)) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func fallbackCurrency(currency string) string {
	if trimmed := strings.TrimSpace(currency); trimmed != "" {
		return trimmed
	}
	return "USD"
}

func resolveSessionID(req ChatCompletionRequest) string {
	if normalized := normalizeOptionalString(req.SessionID); normalized != "" {
		return normalizeExistingIdentifier(normalized, "sess")
	}
	if normalized := normalizeOptionalString(req.User); normalized != "" {
		return normalizeExistingIdentifier(normalized, "sess")
	}
	return ""
}

func normalizeSessionID(value *string, traceID string) string {
	if value != nil {
		if normalized := normalizeExistingIdentifier(*value, "sess"); normalized != "" {
			return normalized
		}
	}
	return normalizeExistingIdentifier("sess_"+traceID, "sess")
}

func deriveSessionTitle(messages []ChatMessage) *string {
	for _, message := range messages {
		if strings.TrimSpace(message.Role) != "user" {
			continue
		}
		content := strings.TrimSpace(flattenChatMessage(message))
		if content == "" {
			continue
		}
		runes := []rune(content)
		if len(runes) > 72 {
			content = string(runes[:72])
		}
		return &content
	}
	return nil
}

func buildPersistedMessages(messages []ChatMessage) []services.PersistedChatMessage {
	currentTurn := currentTurnMessages(messages)
	items := make([]services.PersistedChatMessage, 0, len(currentTurn))
	for index, message := range currentTurn {
		content := strings.TrimSpace(flattenChatMessage(message))
		role := strings.TrimSpace(message.Role)
		if role == "" || content == "" {
			continue
		}
		items = append(items, services.PersistedChatMessage{
			Role:       role,
			Content:    content,
			SequenceNo: index + 1,
		})
	}
	return items
}

func resolveUsage(messages []ChatMessage, responsePayload string, promptTokens, completionTokens, totalTokens int) (int, int, int, bool) {
	if totalTokens > 0 || promptTokens > 0 || completionTokens > 0 {
		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}
		return promptTokens, completionTokens, totalTokens, false
	}

	promptEstimate := estimateTokens(flattenMessages(messages))
	completionEstimate := estimateTokens(responsePayload)
	return promptEstimate, completionEstimate, promptEstimate + completionEstimate, true
}

func estimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	runes := len([]rune(trimmed))
	tokens := runes / 4
	if runes%4 != 0 {
		tokens++
	}
	if tokens == 0 {
		return 1
	}
	return tokens
}

func flattenMessages(messages []ChatMessage) string {
	var parts []string
	for _, message := range messages {
		content := flattenChatMessage(message)
		if content == "" {
			continue
		}
		if message.Role != "" {
			parts = append(parts, message.Role+": "+content)
			continue
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n")
}

func extractToolReferenceIDs(messages []ChatMessage) []string {
	currentTurn := currentTurnMessages(messages)
	if len(currentTurn) > 0 && strings.EqualFold(strings.TrimSpace(currentTurn[0].Role), "user") {
		currentTurn = currentTurn[1:]
	}

	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, message := range currentTurn {
		if normalized := normalizeToolReferenceID(message.ToolCallID); normalized != "" {
			if _, exists := seen[normalized]; !exists {
				seen[normalized] = struct{}{}
				ids = append(ids, normalized)
			}
		}
		for _, toolCall := range message.ToolCalls {
			if normalized := normalizeToolReferenceID(toolCall.ID); normalized != "" {
				if _, exists := seen[normalized]; !exists {
					seen[normalized] = struct{}{}
					ids = append(ids, normalized)
				}
			}
		}
	}
	return ids
}

func normalizeToolReferenceID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(trimmed))
	for _, char := range trimmed {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(unicode.ToLower(char))
		}
	}
	return builder.String()
}

func extractToolReferenceIDsFromPayload(payload string) map[string]struct{} {
	ids := make(map[string]struct{})
	addID := func(value string) {
		if normalized := normalizeToolReferenceID(value); normalized != "" {
			ids[normalized] = struct{}{}
		}
	}

	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return ids
	}

	if strings.HasPrefix(trimmed, "data:") {
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			chunkPayload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if chunkPayload == "" || chunkPayload == "[DONE]" {
				continue
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(chunkPayload), &chunk); err != nil {
				continue
			}

			for _, choice := range chunk.Choices {
				for _, toolCall := range choice.Delta.ToolCalls {
					addID(toolCall.ID)
				}
			}
		}
		return ids
	}

	var response ChatCompletionResponse
	if err := json.Unmarshal([]byte(trimmed), &response); err == nil {
		for _, choice := range response.Choices {
			for _, toolCall := range choice.Message.ToolCalls {
				addID(toolCall.ID)
			}
		}
	}

	return ids
}

func normalizedToolReferenceSet(candidates []string) map[string]struct{} {
	items := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if normalized := normalizeToolReferenceID(candidate); normalized != "" {
			items[normalized] = struct{}{}
		}
	}
	return items
}

func payloadContainsAny(payload string, candidates []string) bool {
	if payload == "" || len(candidates) == 0 {
		return false
	}

	normalizedCandidates := normalizedToolReferenceSet(candidates)
	if len(normalizedCandidates) == 0 {
		return false
	}

	for payloadID := range extractToolReferenceIDsFromPayload(payload) {
		if _, exists := normalizedCandidates[payloadID]; exists {
			return true
		}
	}

	normalizedPayload := normalizeToolReferenceID(payload)
	for candidate := range normalizedCandidates {
		if candidate != "" && strings.Contains(normalizedPayload, candidate) {
			return true
		}
	}
	return false
}

func currentTurnMessages(messages []ChatMessage) []ChatMessage {
	startIndex := 0
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			startIndex = index
			break
		}
	}
	return messages[startIndex:]
}

func normalizeOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func invocationsContainAnyToolReference(items []models.ModelInvocation, toolReferenceIDs []string) bool {
	for _, invocation := range items {
		if invocation.ResponsePayload == nil {
			continue
		}
		if payloadContainsAny(*invocation.ResponsePayload, toolReferenceIDs) {
			return true
		}
	}
	return false
}

func logPersistenceError(action, traceID string, err error) {
	if err == nil {
		return
	}
	if strings.TrimSpace(traceID) == "" {
		log.Printf("ai gateway: %s: %v", action, err)
		return
	}
	log.Printf("ai gateway: %s for trace %s: %v", action, traceID, err)
}

func flattenMessageContent(content interface{}) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if text := flattenStructuredContentPart(item); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
		return mustJSONString(value)
	case map[string]interface{}:
		if text := flattenStructuredContentPart(value); text != "" {
			return text
		}
		return mustJSONString(value)
	default:
		return mustJSONString(value)
	}
}

func flattenStructuredContentPart(content interface{}) string {
	part, ok := content.(map[string]interface{})
	if !ok {
		return ""
	}

	if text, ok := part["text"].(string); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := part["input_text"].(string); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := part["output_text"].(string); ok {
		return strings.TrimSpace(text)
	}

	return ""
}

func flattenChatMessage(message ChatMessage) string {
	parts := []string{}

	content := strings.TrimSpace(flattenMessageContent(message.Content))
	if content != "" {
		parts = append(parts, content)
	}

	toolCalls := strings.TrimSpace(flattenToolCalls(message.ToolCalls))
	if toolCalls != "" {
		parts = append(parts, toolCalls)
	}

	return strings.Join(parts, "\n")
}

func flattenToolCalls(toolCalls []ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	parts := make([]string, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		if toolCall.Function == nil {
			parts = append(parts, mustJSONString(toolCall))
			continue
		}

		name := strings.TrimSpace(toolCall.Function.Name)
		args := strings.TrimSpace(toolCall.Function.Arguments)
		switch {
		case name != "" && args != "":
			parts = append(parts, fmt.Sprintf("tool_call %s(%s)", name, args))
		case name != "":
			parts = append(parts, "tool_call "+name)
		default:
			parts = append(parts, mustJSONString(toolCall))
		}
	}

	return strings.Join(parts, "\n")
}

func extractAssistantContent(response ChatCompletionResponse) string {
	var parts []string
	for _, choice := range response.Choices {
		content := strings.TrimSpace(flattenMessageContent(choice.Message.Content))
		if content != "" {
			parts = append(parts, content)
			continue
		}

		toolCalls := strings.TrimSpace(flattenToolCalls(choice.Message.ToolCalls))
		if toolCalls != "" {
			parts = append(parts, toolCalls)
		}
	}
	return strings.Join(parts, "\n")
}
