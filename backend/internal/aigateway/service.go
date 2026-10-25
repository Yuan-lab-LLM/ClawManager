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
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"clawreef/internal/models"
	"clawreef/internal/ownerrecords"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"
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
	ImageIdempotencyKey string                            `json:"-"`
	RuntimeTask         *models.GatewayTaskIdentity       `json:"-"`
	RawBody             []byte                            `json:"-"`
	Model               string                            `json:"model"`
	Messages            []ChatMessage                     `json:"messages"`
	Temperature         *float64                          `json:"temperature,omitempty"`
	TopP                *float64                          `json:"top_p,omitempty"`
	MaxTokens           *int                              `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                              `json:"max_completion_tokens,omitempty"`
	Stream              bool                              `json:"stream"`
	Tools               json.RawMessage                   `json:"tools,omitempty"`
	ToolChoice          json.RawMessage                   `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool                             `json:"parallel_tool_calls,omitempty"`
	ResponseFormat      json.RawMessage                   `json:"response_format,omitempty"`
	Stop                json.RawMessage                   `json:"stop,omitempty"`
	N                   *int                              `json:"n,omitempty"`
	FrequencyPenalty    *float64                          `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64                          `json:"presence_penalty,omitempty"`
	ReasoningEffort     *string                           `json:"reasoning_effort,omitempty"`
	Thinking            json.RawMessage                   `json:"thinking,omitempty"`
	StreamOptions       json.RawMessage                   `json:"stream_options,omitempty"`
	User                *string                           `json:"user,omitempty"`
	SessionID           *string                           `json:"session_id,omitempty"`
	OpenClawSessionKey  *string                           `json:"-"`
	ManagedAgentType    *string                           `json:"-"`
	InstanceID          *int                              `json:"instance_id,omitempty"`
	InstanceMode        *string                           `json:"instance_mode,omitempty"`
	RuntimeType         *string                           `json:"runtime_type,omitempty"`
	GatewayID           *string                           `json:"gateway_id,omitempty"`
	RuntimePodID        *int64                            `json:"runtime_pod_id,omitempty"`
	TraceID             *string                           `json:"trace_id,omitempty"`
	RequestID           *string                           `json:"request_id,omitempty"`
	AllowedModelGroups  []string                          `json:"-"`
	UsageFinalizer      func(int) error                   `json:"-"`
	GatewayKeyID        *int                              `json:"-"`
	BudgetController    *services.GatewayBudgetController `json:"-"`
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
const anthropicVersionHeader = "2023-06-01"
const defaultAnthropicMaxTokens = defaultGatewayOutputTokens
const providerRequestTimeout = 3 * time.Minute
const upstreamStreamInterruptedCode = "upstream_stream_interrupted"
const maxDeferredStreamPreludeBytes = 256 * 1024

var providerVersionSegmentPattern = regexp.MustCompile(`(?i)^v\d+(?:[a-z0-9._-]*)?$`)

type preparedChatRequest struct {
	traceID                 string
	sessionID               string
	sessionIDPtr            *string
	requestID               string
	requestIDPtr            *string
	userID                  int
	userIDPtr               *int
	selectedModel           *models.LLMModel
	resolvedModel           *models.LLMModel
	deploymentCandidates    []*models.LLMModel
	attemptNumber           int
	attemptTTFTMs           int
	attemptGenerationMs     int
	attemptTokensPerSecond  float64
	attemptStatusCode       int
	attemptFailureResponse  *ProxyResponse
	attemptSucceeded        bool
	attemptDispatched       bool
	attemptPromptTokens     int
	attemptCompletionTokens int
	priorAttemptTokens      int
	priorAttemptCost        float64
	req                     ChatCompletionRequest
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	Model   string `json:"model,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string      `json:"role,omitempty"`
			Content          interface{} `json:"content,omitempty"`
			ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			Refusal          interface{} `json:"refusal,omitempty"`
			ReasoningContent interface{} `json:"reasoning_content,omitempty"`
			Reasoning        interface{} `json:"reasoning,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message,omitempty"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

type openAIStreamInspection struct {
	Done          bool
	Finished      bool
	Semantic      bool
	ProviderError error
}

type openAIStreamProxyResult struct {
	RawStream        string
	AssistantText    string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	SawDone          bool
	SawFinishReason  bool
	Terminal         bool
	FirstTokenAt     time.Time
}

type upstreamStreamReadError struct {
	err error
}

func (e *upstreamStreamReadError) Error() string {
	return fmt.Sprintf("failed while reading provider stream: %v", e.err)
}

func (e *upstreamStreamReadError) Unwrap() error {
	return e.err
}

type upstreamStreamProviderError struct {
	message string
}

func (e *upstreamStreamProviderError) Error() string {
	return e.message
}

type downstreamStreamWriteError struct {
	err error
}

func (e *downstreamStreamWriteError) Error() string {
	return fmt.Sprintf("failed while writing provider stream: %v", e.err)
}

func (e *downstreamStreamWriteError) Unwrap() error {
	return e.err
}

// deferredStreamWriter holds protocol prelude/heartbeat frames until either a
// semantic token (text, reasoning, refusal or tool call) or a valid terminal event is observed.
// This keeps the downstream response uncommitted while another deployment can
// still be selected safely.
type deferredStreamWriter struct {
	writer    io.Writer
	flusher   http.Flusher
	pending   bytes.Buffer
	committed bool
}

func (w *deferredStreamWriter) Write(payload []byte) (int, error) {
	if w.committed {
		return w.writer.Write(payload)
	}
	if w.pending.Len()+len(payload) > maxDeferredStreamPreludeBytes {
		return 0, errors.New("upstream stream prelude exceeded the safety limit before producing a semantic token")
	}
	return w.pending.Write(payload)
}

func (w *deferredStreamWriter) Flush() {
	if w.committed {
		w.flusher.Flush()
	}
}

func (w *deferredStreamWriter) Commit() error {
	if w.committed {
		return nil
	}
	w.committed = true
	if w.pending.Len() > 0 {
		if _, err := w.writer.Write(w.pending.Bytes()); err != nil {
			return err
		}
		w.pending.Reset()
	}
	w.flusher.Flush()
	return nil
}

type openAIStreamDelta struct {
	Role      string      `json:"role,omitempty"`
	Content   interface{} `json:"content,omitempty"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason,omitempty"`
}

type openAIStreamResponseChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object,omitempty"`
	Created int64                `json:"created,omitempty"`
	Model   string               `json:"model,omitempty"`
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type anthropicRequestMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicRequestPayload struct {
	Model       string                    `json:"model"`
	Messages    []anthropicRequestMessage `json:"messages"`
	System      string                    `json:"system,omitempty"`
	MaxTokens   int                       `json:"max_tokens"`
	Stream      bool                      `json:"stream,omitempty"`
	Temperature *float64                  `json:"temperature,omitempty"`
	TopP        *float64                  `json:"top_p,omitempty"`
	StopSeqs    []string                  `json:"stop_sequences,omitempty"`
	Tools       []anthropicToolDefinition `json:"tools,omitempty"`
	ToolChoice  map[string]interface{}    `json:"tool_choice,omitempty"`
}

type anthropicToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

type anthropicContentBlock struct {
	Type      string      `json:"type"`
	Source    interface{} `json:"source,omitempty"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   interface{} `json:"content,omitempty"`
}

type anthropicMessageResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type,omitempty"`
	Role       string                  `json:"role,omitempty"`
	Model      string                  `json:"model,omitempty"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason,omitempty"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicStreamEvent struct {
	Type         string                    `json:"type"`
	Message      *anthropicMessageResponse `json:"message,omitempty"`
	Index        *int                      `json:"index,omitempty"`
	ContentBlock *anthropicContentBlock    `json:"content_block,omitempty"`
	Delta        *struct {
		Type         string `json:"type,omitempty"`
		Text         string `json:"text,omitempty"`
		PartialJSON  string `json:"partial_json,omitempty"`
		StopReason   string `json:"stop_reason,omitempty"`
		StopSequence string `json:"stop_sequence,omitempty"`
	} `json:"delta,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Type    string `json:"type,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

type anthropicStreamState struct {
	ResponseID       string
	Model            string
	Created          int64
	PromptTokens     int
	CompletionTokens int
	AssistantText    strings.Builder
	ToolCalls        map[int]*anthropicToolCallState
	SentRole         bool
	FirstTokenAt     time.Time
}

type anthropicToolCallState struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// Service defines gateway operations.
type Service interface {
	ListAvailableModels() ([]AvailableModel, error)
	ChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest) (*ProxyResponse, string, error)
	StreamChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest, w http.ResponseWriter) (string, error)
}

type service struct {
	ownerUsage               *ownerrecords.Store
	expandedModelCatalog     services.ExpandedLLMModelCatalog
	runtimeTasks             repository.GatewayTaskRepository
	imageArtifacts           services.GatewayImageArtifactService
	imageJobs                services.GatewayImageJobStore
	modelAccessChecker       repository.ModelAccessChecker
	modelRepo                repository.LLMModelRepository
	invocationService        services.ModelInvocationService
	auditEventService        services.AuditEventService
	costRecordService        services.CostRecordService
	riskDetector             services.RiskDetectionService
	riskHitService           services.RiskHitService
	chatSessionService       services.ChatSessionService
	chatMessageService       services.ChatMessageService
	secretRefService         services.SecretRefService
	deploymentStore          services.GatewayDeploymentStore
	routingClassifier        *routingClassifierRunner
	resolveRoutingClassifier func(context.Context) (*routingClassifierRunner, error)
	httpClient               *http.Client
	streamHTTPClient         *http.Client
}

// ServiceOption configures optional gateway runtime coordination.
type ServiceOption func(*service)

// WithDeploymentStore enables shared health and load-aware deployment routing.
func WithDeploymentStore(store services.GatewayDeploymentStore) ServiceOption {
	return func(target *service) {
		if store != nil {
			target.deploymentStore = store
		}
	}
}

func WithOwnerUsage(store *ownerrecords.Store) ServiceOption {
	return func(s *service) { s.ownerUsage = store }
}

func WithExpandedLLMModelCatalog(catalog services.ExpandedLLMModelCatalog) ServiceOption {
	return func(s *service) {
		s.expandedModelCatalog = catalog
	}
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
	options ...ServiceOption,
) Service {
	httpClient, streamHTTPClient := newGatewayHTTPClients()
	target := &service{
		modelRepo:          modelRepo,
		invocationService:  invocationService,
		auditEventService:  auditEventService,
		costRecordService:  costRecordService,
		riskDetector:       riskDetector,
		riskHitService:     riskHitService,
		chatSessionService: chatSessionService,
		chatMessageService: chatMessageService,
		secretRefService:   services.NewSecretRefService(),
		deploymentStore:    services.NewNoopGatewayDeploymentStore(),
		httpClient:         httpClient,
		streamHTTPClient:   streamHTTPClient,
	}
	for _, option := range options {
		if option != nil {
			option(target)
		}
	}
	return target
}

func newGatewayHTTPClients() (*http.Client, *http.Client) {
	return &http.Client{
			Timeout: providerRequestTimeout,
		}, &http.Client{
			// Streaming model calls are bounded by the incoming request context,
			// not by a fixed whole-request deadline. A single Agent turn may
			// legitimately take longer than an ordinary JSON request.
			Timeout: 0,
		}
}

func (s *service) ListAvailableModels() ([]AvailableModel, error) {
	items, err := s.listActiveModels()
	if err != nil {
		return nil, fmt.Errorf("failed to list available models: %w", err)
	}
	items = models.ChatModels(items)
	if len(items) == 0 {
		return []AvailableModel{}, nil
	}

	available := []AvailableModel{
		{
			ID:          0,
			DisplayName: "Auto",
			Description: stringPtr("Automatically route requests to the best available model under current governance policy."),
			IsSecure:    false,
			Provider:    "gateway",
		},
	}
	seen := map[string]struct{}{autoModelID: {}}
	for _, item := range items {
		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(item.ProviderModelName)
		}
		if displayName == "" {
			continue
		}

		normalizedName := strings.ToLower(displayName)
		if _, exists := seen[normalizedName]; exists {
			continue
		}
		seen[normalizedName] = struct{}{}
		available = append(available, AvailableModel{
			ID:          item.ID,
			DisplayName: displayName,
			Description: item.Description,
			IsSecure:    item.IsSecure,
			Provider:    item.ProviderType,
		})
	}

	return available, nil
}

func (s *service) listActiveModels() ([]models.LLMModel, error) {
	// The repository is scoped to the instance owner on request entry. A
	// provider discovery catalog is not an authorization source: it must never
	// add unconfigured models or bypass the scoped repository.
	return s.modelRepo.ListActive()
}

func (s *service) ChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest) (*ProxyResponse, string, error) {
	if err := s.checkRuntimeTask(ctx, req); err != nil {
		return nil, "", err
	}
	scoped := *s
	scoped.modelAccessChecker, _ = s.modelRepo.(repository.ModelAccessChecker)
	scoped.modelRepo = repository.ForModelUser(s.modelRepo, userID)
	s = &scoped
	prepared, err := s.prepareChatRequest(ctx, userID, req)
	if err != nil {
		traceID := ""
		if prepared != nil {
			traceID = prepared.traceID
		}
		return nil, traceID, err
	}

	candidates := prepared.deploymentCandidates
	if len(candidates) == 0 {
		candidates = []*models.LLMModel{prepared.resolvedModel}
	}
	var lastResponse *ProxyResponse
	var lastErr error
	for index, candidate := range candidates {
		prepared.resolvedModel = candidate
		callCtx, stopGuard, accessErr := guardModelCall(ctx, s.taskAccessChecker(prepared.req), userID, candidate.ID)
		if accessErr != nil {
			s.finalizeFailedAttemptAccounting(prepared)
			s.recordAccessInterruption(prepared, accessErr)
			return nil, prepared.traceID, accessErr
		}
		lease, acquireErr := s.beginDeploymentAttempt(callCtx, prepared, candidate)
		if errors.Is(acquireErr, services.ErrGatewayDeploymentUnavailable) {
			stopGuard()
			lastErr = acquireErr
			if index+1 < len(candidates) {
				s.recordDeploymentFailover(prepared, candidate, candidates[index+1], nil, acquireErr)
			}
			continue
		}
		// Capacity skips must not bind a deployment that this turn never called.
		if err := s.bindRuntimeTask(callCtx, prepared.req, candidate.ID); err != nil {
			stopGuard()
			s.finishDeploymentAttempt(prepared, candidate, lease, 0, false, false)
			s.finalizeFailedAttemptAccounting(prepared)
			s.recordAccessInterruption(prepared, err)
			return nil, prepared.traceID, err
		}
		attemptStartedAt := time.Now()
		response, traceID, callErr := s.callNonStreamingDeployment(callCtx, prepared)
		accessCause := context.Cause(callCtx)
		stopGuard()
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		prepared.attemptStatusCode = statusCode
		success := callErr == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
		s.finishDeploymentAttempt(prepared, candidate, lease, time.Since(attemptStartedAt), success && accessCause == nil, accessCause == nil && shouldPenalizeDeployment(ctx, statusCode, callErr, response))
		if accessCause != nil {
			s.finalizeFailedAttemptAccounting(prepared)
			if ctx.Err() == nil {
				s.recordAccessInterruption(prepared, accessCause)
			}
			return nil, traceID, accessCause
		}
		if callErr == nil && !isRetryableProviderResponse(response) {
			if !success {
				s.finalizeFailedAttemptAccounting(prepared)
			}
			return response, traceID, nil
		}
		lastResponse, lastErr = response, callErr
		if index+1 < len(candidates) {
			s.recordDeploymentFailover(prepared, candidate, candidates[index+1], response, callErr)
			continue
		}
		if callErr != nil {
			s.finalizeFailedAttemptAccounting(prepared)
			return nil, traceID, callErr
		}
		s.finalizeFailedAttemptAccounting(prepared)
		return response, traceID, nil
	}
	s.finalizeFailedAttemptAccounting(prepared)
	return lastResponse, prepared.traceID, lastErr
}

func (s *service) callNonStreamingDeployment(ctx context.Context, prepared *preparedChatRequest) (*ProxyResponse, string, error) {
	switch models.ResolveLLMProtocolTypeOrDefault(prepared.resolvedModel.ProviderType, prepared.resolvedModel.ProtocolType) {
	case models.ProtocolTypeOpenAI, models.ProtocolTypeOpenAICompatible:
		return s.callOpenAICompatible(ctx, prepared)
	case models.ProtocolTypeAnthropic:
		return s.callAnthropic(ctx, prepared)
	default:
		return nil, prepared.traceID, fmt.Errorf("provider type %s is not supported yet", prepared.resolvedModel.ProviderType)
	}
}

func isRetryableProviderResponse(response *ProxyResponse) bool {
	if response == nil {
		return false
	}
	return response.StatusCode == http.StatusRequestTimeout ||
		isProviderModelPermissionDenied(response) ||
		response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusPaymentRequired ||
		response.StatusCode == http.StatusNotFound ||
		response.StatusCode == http.StatusConflict ||
		response.StatusCode == http.StatusTooEarly ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError
}

func (s *service) recordDeploymentFailover(prepared *preparedChatRequest, failed, next *models.LLMModel, response *ProxyResponse, callErr error) {
	if prepared == nil || failed == nil || next == nil {
		return
	}
	reason := "provider call failed"
	if callErr != nil {
		reason = callErr.Error()
	} else if response != nil {
		reason = fmt.Sprintf("provider status %d", response.StatusCode)
	}
	details, _ := json.Marshal(map[string]interface{}{
		"failed_attempt": prepared.attemptNumber,
		"failed_model":   failed.ProviderModelName,
		"next_model":     next.ProviderModelName,
	})
	if err := s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID: prepared.traceID, SessionID: prepared.sessionIDPtr, RequestID: prepared.requestIDPtr,
		UserID: prepared.userIDPtr, InstanceID: prepared.req.InstanceID,
		GatewayKeyID: prepared.req.GatewayKeyID,
		InstanceMode: runtimeAttributionString(prepared.req.InstanceMode), RuntimeType: runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID: runtimeAttributionString(prepared.req.GatewayID), RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
		EventType: "gateway.deployment.failover", TrafficClass: models.TrafficClassLLM, Severity: models.AuditSeverityWarn,
		Message: fmt.Sprintf("Failing over model group %s from %s to %s: %s", failed.ModelGroup, failed.DisplayName, next.DisplayName, reason),
		Details: stringPtr(string(details)),
	}); err != nil {
		logPersistenceError("record gateway.deployment.failover", prepared.traceID, err)
	}
}

func (s *service) StreamChatCompletions(ctx context.Context, userID int, req ChatCompletionRequest, w http.ResponseWriter) (string, error) {
	if err := s.checkRuntimeTask(ctx, req); err != nil {
		return "", err
	}
	scoped := *s
	scoped.modelAccessChecker, _ = s.modelRepo.(repository.ModelAccessChecker)
	scoped.modelRepo = repository.ForModelUser(s.modelRepo, userID)
	s = &scoped
	prepared, err := s.prepareChatRequest(ctx, userID, req)
	if err != nil {
		traceID := ""
		if prepared != nil {
			traceID = prepared.traceID
		}
		return traceID, err
	}
	return prepared.traceID, s.streamPreparedChatCompletions(ctx, prepared, w)
}

func (s *service) streamPreparedChatCompletions(ctx context.Context, prepared *preparedChatRequest, w http.ResponseWriter) (resultErr error) {
	// An attempt failure is not necessarily a request failure. Record a separate
	// terminal event so observers can distinguish failover from a stopped request.
	defer func() {
		if s.auditEventService == nil || (resultErr == nil && prepared.attemptSucceeded) {
			return
		}
		_ = s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID: prepared.traceID, RequestID: stringPtr(prepared.requestID),
			InstanceID: prepared.req.InstanceID, SessionID: prepared.sessionIDPtr,
			UserID: prepared.userIDPtr, GatewayKeyID: prepared.req.GatewayKeyID,
			EventType: "gateway.request.stream_failed", TrafficClass: models.TrafficClassLLM,
			Severity: models.AuditSeverityWarn, Message: "Streaming request ended without completing successfully; no further deployment retry is running.",
		})
	}()
	candidates := prepared.deploymentCandidates
	if len(candidates) == 0 {
		candidates = []*models.LLMModel{prepared.resolvedModel}
	}
	trackedWriter := &trackingResponseWriter{ResponseWriter: w}
	var lastErr error
	for index, candidate := range candidates {
		prepared.resolvedModel = candidate
		callCtx, stopGuard, accessErr := guardModelCall(ctx, s.taskAccessChecker(prepared.req), prepared.userID, candidate.ID)
		if accessErr != nil {
			s.finalizeFailedAttemptAccounting(prepared)
			s.recordAccessInterruption(prepared, accessErr)
			return accessErr
		}
		lease, acquireErr := s.beginDeploymentAttempt(callCtx, prepared, candidate)
		if errors.Is(acquireErr, services.ErrGatewayDeploymentUnavailable) {
			stopGuard()
			lastErr = acquireErr
			if index+1 < len(candidates) {
				s.recordDeploymentFailover(prepared, candidate, candidates[index+1], nil, acquireErr)
			}
			continue
		}
		if err := s.bindRuntimeTask(callCtx, prepared.req, candidate.ID); err != nil {
			stopGuard()
			s.finishDeploymentAttempt(prepared, candidate, lease, 0, false, false)
			s.finalizeFailedAttemptAccounting(prepared)
			s.recordAccessInterruption(prepared, err)
			return err
		}
		attemptStartedAt := time.Now()
		hasFallback := index+1 < len(candidates)
		callErr := s.streamDeployment(callCtx, prepared, trackedWriter, hasFallback)
		accessCause := context.Cause(callCtx)
		stopGuard()
		s.finishDeploymentAttempt(prepared, candidate, lease, time.Since(attemptStartedAt), prepared.attemptSucceeded && accessCause == nil, accessCause == nil && shouldPenalizeDeployment(ctx, prepared.attemptStatusCode, callErr, prepared.attemptFailureResponse))
		if accessCause != nil {
			s.finalizeFailedAttemptAccounting(prepared)
			if ctx.Err() == nil {
				s.recordAccessInterruption(prepared, accessCause)
			}
			if trackedWriter.committed() && errors.Is(accessCause, models.ErrModelUnavailable) {
				writeModelAccessStreamError(w)
			}
			return accessCause
		}
		if callErr == nil {
			if !prepared.attemptSucceeded {
				s.finalizeFailedAttemptAccounting(prepared)
			}
			return nil
		}
		lastErr = callErr
		if trackedWriter.committed() || !hasFallback {
			s.finalizeFailedAttemptAccounting(prepared)
			return callErr
		}

		var response *ProxyResponse
		var statusErr *retryableStreamResponseError
		if errors.As(callErr, &statusErr) {
			response = &ProxyResponse{StatusCode: statusErr.StatusCode}
		}
		s.recordDeploymentFailover(prepared, candidate, candidates[index+1], response, callErr)
	}
	s.finalizeFailedAttemptAccounting(prepared)
	return lastErr
}

func (s *service) streamDeployment(ctx context.Context, prepared *preparedChatRequest, w http.ResponseWriter, deferRetryableResponse bool) error {
	switch models.ResolveLLMProtocolTypeOrDefault(prepared.resolvedModel.ProviderType, prepared.resolvedModel.ProtocolType) {
	case models.ProtocolTypeOpenAI, models.ProtocolTypeOpenAICompatible:
		return s.streamOpenAICompatible(ctx, prepared, w, deferRetryableResponse)
	case models.ProtocolTypeAnthropic:
		return s.streamAnthropic(ctx, prepared, w, deferRetryableResponse)
	default:
		if err := s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.req.SessionID,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			GatewayKeyID: prepared.req.GatewayKeyID,
			InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
			RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
			GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
			RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
			EventType:    "gateway.request.blocked",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      fmt.Sprintf("Provider type %s is not supported yet", prepared.resolvedModel.ProviderType),
		}); err != nil {
			logPersistenceError("record gateway.request.blocked", prepared.traceID, err)
		}
		return errors.New("provider type is not supported yet")
	}
}

type trackingResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	wroteBody   bool
}

func (w *trackingResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *trackingResponseWriter) Write(payload []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.wroteBody = true
	return w.ResponseWriter.Write(payload)
}

func (w *trackingResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *trackingResponseWriter) committed() bool {
	return w.wroteHeader || w.wroteBody
}

type retryableStreamResponseError struct {
	StatusCode int
	Message    string
}

func (e *retryableStreamResponseError) Error() string {
	return fmt.Sprintf("provider returned retryable status %d: %s", e.StatusCode, e.Message)
}

func (s *service) prepareChatRequest(ctx context.Context, userID int, req ChatCompletionRequest) (*preparedChatRequest, error) {
	if err := NormalizeVisionRequest(&req); err != nil {
		return nil, err
	}
	if err := NormalizeGenerationLimits(&req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("model is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("messages are required")
	}

	requestedModel := strings.TrimSpace(req.Model)
	if s.ownerUsage != nil && req.InstanceID != nil && *req.InstanceID > 0 {
		s.ownerUsage.QueueBinding(uint64(*req.InstanceID))
	}
	var selectedModel *models.LLMModel
	var err error
	if !isAutoModelRequest(requestedModel) {
		selectedModel, err = s.resolveRequestedModel(requestedModel)
	}
	if err != nil {
		return nil, err
	}
	if !modelAllowedForGatewayKey(selectedModel, req.AllowedModelGroups) {
		return nil, errors.New("gateway key is not allowed to access the requested model group")
	}

	prepared := &preparedChatRequest{
		userID:        userID,
		userIDPtr:     intPtr(userID),
		selectedModel: selectedModel,
		req:           req,
	}
	sessionSource, sessionID := resolveSessionIdentity(req)
	prepared.sessionID = sessionID
	if prepared.sessionID != "" && sessionSource != "openclaw_header" {
		if runtimeType := strings.TrimSpace(sessionNormalizationRuntimeType(req)); runtimeType != "" {
			prepared.sessionID = utils.NormalizeOpenClawSessionID(prepared.sessionID, runtimeType)
		}
	}
	prepared.traceID = s.resolveTraceID(userID, req, prepared.sessionID)
	if prepared.sessionID == "" {
		prepared.sessionID = normalizeSessionID(nil, prepared.traceID)
		sessionSource = "trace_fallback"
	}
	prepared.sessionIDPtr = stringPtr(prepared.sessionID)
	prepared.req.SessionID = prepared.sessionIDPtr
	prepared.requestID = normalizeOrCreateID(req.RequestID, "req")
	prepared.requestIDPtr = stringPtr(prepared.requestID)

	if sessionSource == "trace_fallback" && req.InstanceID != nil {
		if err := s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.sessionIDPtr,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   req.InstanceID,
			GatewayKeyID: req.GatewayKeyID,
			InstanceMode: runtimeAttributionString(req.InstanceMode),
			RuntimeType:  runtimeAttributionString(req.RuntimeType),
			GatewayID:    runtimeAttributionString(req.GatewayID),
			RuntimePodID: runtimeAttributionInt64(req.RuntimePodID),
			EventType:    "gateway.session.fallback",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      fmt.Sprintf("LLM request missing stable session key for instance %d", *req.InstanceID),
		}); err != nil {
			logPersistenceError("record gateway.session.fallback", prepared.traceID, err)
		}
	}

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
		GatewayKeyID: prepared.req.GatewayKeyID,
		InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
		RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
		RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
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
			GatewayKeyID: prepared.req.GatewayKeyID,
			InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
			RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
			GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
			RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
			EventType:    "gateway.risk.detected",
			TrafficClass: models.TrafficClassLLM,
			Severity:     models.AuditSeverityWarn,
			Message:      fmt.Sprintf("Sensitive content detected with %d hit(s)", len(riskAnalysis.Hits)),
		}); err != nil {
			logPersistenceError("record gateway.risk.detected", prepared.traceID, err)
		}
	}

	var resolvedModel *models.LLMModel
	var resolveErr error
	riskAction := models.RiskActionAllow
	var routing *routingDecision
	if isAutoModelRequest(requestedModel) {
		if riskAnalysis.IsSensitive && (riskAnalysis.HighestAction == "require_approval" || riskAnalysis.HighestAction == models.RiskActionBlock) {
			resolveErr = errors.New("request was blocked by risk policy")
			d := classifyRoutingDifficulty(req.Messages)
			d.SecureRequired = true
			d.PromptTokens, d.OutputTokens = routingTokenEstimate(req)
			routing = &d
		} else {
			secureRequired := riskAnalysis.IsSensitive && riskAnalysis.HighestAction == models.RiskActionRouteSecureModel
			resolvedModel, routing, resolveErr = s.selectContentModel(ctx, prepared.req, secureRequired)
			if secureRequired {
				riskAction = models.RiskActionRouteSecureModel
			}
		}
		if resolveErr != nil {
			riskAction = models.RiskActionBlock
			if routing != nil {
				routing.FailureReason = resolveErr.Error()
				s.recordRoutingDecision(prepared, routing)
			}
		}
	} else {
		resolvedModel, riskAction, resolveErr = s.resolveTargetModelForRequest(prepared.selectedModel, riskAnalysis, req)
		if resolveErr == nil && requestRequiresVision(req) && (!models.LLMVisionSupported(prepared.selectedModel) || !models.LLMVisionSupported(resolvedModel)) {
			resolveErr = models.ErrVisionNotSupported
		}
		if resolveErr == nil && !modelTransportSupported(resolvedModel, req) {
			resolveErr = errors.New("provider protocol or completion count is not supported")
			if requestRequiresVision(req) {
				resolveErr = invalidImage("selected protocol cannot preserve this image role or detail option")
			}
		}
		if resolveErr == nil && req.BudgetController != nil && routingCurrency(resolvedModel) != services.GatewayBudgetCurrency {
			resolveErr = services.ErrGatewayBudgetCurrencyMismatch
		}
	}
	if resolveErr != nil {
		blockedInvocationID := s.recordBlockedInvocation(prepared.traceID, prepared.requestID, prepared.req, prepared.userID, prepared.selectedModel, resolveErr.Error())
		if err := s.riskHitService.RecordHits(prepared.traceID, prepared.req.SessionID, prepared.requestIDPtr, prepared.userIDPtr, prepared.req.InstanceID, blockedInvocationID, riskHitAttribution(prepared.req), riskAction, riskAnalysis.Hits); err != nil {
			logPersistenceError("record blocked risk hits", prepared.traceID, err)
		}
		if err := s.auditEventService.RecordEvent(&models.AuditEvent{
			TraceID:      prepared.traceID,
			SessionID:    prepared.req.SessionID,
			RequestID:    prepared.requestIDPtr,
			UserID:       prepared.userIDPtr,
			InstanceID:   prepared.req.InstanceID,
			GatewayKeyID: prepared.req.GatewayKeyID,
			InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
			RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
			GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
			RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
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
		if err := s.riskHitService.RecordHits(prepared.traceID, prepared.req.SessionID, prepared.requestIDPtr, prepared.userIDPtr, prepared.req.InstanceID, nil, riskHitAttribution(prepared.req), riskAction, riskAnalysis.Hits); err != nil {
			logPersistenceError("record risk hits", prepared.traceID, err)
		}
		if riskAction == models.RiskActionRouteSecureModel && resolvedModel != nil && prepared.selectedModel != nil && resolvedModel.ID != prepared.selectedModel.ID {
			if err := s.auditEventService.RecordEvent(&models.AuditEvent{
				TraceID:      prepared.traceID,
				SessionID:    prepared.req.SessionID,
				RequestID:    prepared.requestIDPtr,
				UserID:       prepared.userIDPtr,
				InstanceID:   prepared.req.InstanceID,
				GatewayKeyID: prepared.req.GatewayKeyID,
				InstanceMode: runtimeAttributionString(prepared.req.InstanceMode),
				RuntimeType:  runtimeAttributionString(prepared.req.RuntimeType),
				GatewayID:    runtimeAttributionString(prepared.req.GatewayID),
				RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
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
	prepared.deploymentCandidates = s.deploymentCandidates(resolvedModel)
	// Explicit routes must obey the same transport/currency constraints during failover.
	usable := make([]*models.LLMModel, 0, len(prepared.deploymentCandidates))
	for _, candidate := range prepared.deploymentCandidates {
		if modelTransportSupported(candidate, req) && (!requestRequiresVision(req) || models.LLMVisionSupported(candidate)) && (req.BudgetController == nil || routingCurrency(candidate) == services.GatewayBudgetCurrency) {
			usable = append(usable, candidate)
		}
	}
	prepared.deploymentCandidates = usable
	if routing != nil {
		filtered := make([]*models.LLMModel, 0, len(prepared.deploymentCandidates))
		for _, candidate := range prepared.deploymentCandidates {
			if routingCandidateAllowed(candidate, prepared.req, *routing) {
				filtered = append(filtered, candidate)
			}
		}
		prepared.deploymentCandidates = prioritizeSelectedDeployment(filtered, resolvedModel)
	}
	if strings.EqualFold(strings.TrimSpace(requestedModel), strings.TrimSpace(resolvedModel.DisplayName)) {
		prepared.deploymentCandidates = prioritizeSelectedDeployment(prepared.deploymentCandidates, resolvedModel)
	}
	if isAutoModelRequest(requestedModel) {
		prepared.deploymentCandidates = s.orderAutoDeploymentGroups(ctx, prepared, routing, prepared.deploymentCandidates)
		if len(prepared.deploymentCandidates) == 0 {
			if routing != nil {
				routing.FailureReason = services.ErrGatewayDeploymentUnavailable.Error()
				s.recordRoutingDecision(prepared, routing)
			}
			return prepared, services.ErrGatewayDeploymentUnavailable
		}
		if len(prepared.deploymentCandidates) > 0 {
			prepared.resolvedModel = prepared.deploymentCandidates[0]
			resolvedModel = prepared.resolvedModel
			if routing != nil {
				contentSelectedGroup := routing.ModelGroup
				routing.ModelID = resolvedModel.ID
				routing.ModelGroup = routingModelGroup(resolvedModel)
				routing.SelectedTier = effectiveQualityTier(resolvedModel)
				routing.EstimatedCost = calculateEstimatedCost(resolvedModel, routing.PromptTokens, routing.OutputTokens)
				routing.Currency = routingCurrency(resolvedModel)
				if !strings.EqualFold(strings.TrimSpace(contentSelectedGroup), strings.TrimSpace(routing.ModelGroup)) {
					routing.Warnings = append(routing.Warnings, "primary_group_unavailable_health_fallback")
				}
			}
		}
	}
	if routing != nil {
		s.recordRoutingDecision(prepared, routing)
	}
	if prepared.req.BudgetController != nil {
		estimatedCost := estimateGatewayBudgetCost(prepared.req, prepared.deploymentCandidates)
		if err := prepared.req.BudgetController.Reserve(ctx, estimatedCost, routingCurrency(resolvedModel)); err != nil {
			reason := err.Error()
			blockedInvocationID := s.recordBlockedInvocation(prepared.traceID, prepared.requestID, prepared.req, prepared.userID, prepared.resolvedModel, reason)
			if auditErr := s.auditEventService.RecordEvent(&models.AuditEvent{
				TraceID: prepared.traceID, SessionID: prepared.sessionIDPtr, RequestID: prepared.requestIDPtr,
				UserID: prepared.userIDPtr, InstanceID: prepared.req.InstanceID, GatewayKeyID: prepared.req.GatewayKeyID,
				InstanceMode: runtimeAttributionString(prepared.req.InstanceMode), RuntimeType: runtimeAttributionString(prepared.req.RuntimeType), GatewayID: runtimeAttributionString(prepared.req.GatewayID), RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
				InvocationID: blockedInvocationID, EventType: "gateway.budget.exceeded", TrafficClass: models.TrafficClassLLM,
				Severity: models.AuditSeverityWarn, Message: reason,
			}); auditErr != nil {
				logPersistenceError("record gateway.budget.exceeded", prepared.traceID, auditErr)
			}
			return prepared, err
		}
	}
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
	prepared.attemptDispatched = true
	response, err := s.httpClient.Do(httpRequest)
	if err != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("provider call failed: %v", err), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return nil, prepared.traceID, fmt.Errorf("failed to call provider: %w", err)
	}
	defer response.Body.Close()

	responseBody, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("failed to read provider response: %v", readErr), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
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
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, "provider returned non-success status: "+message, providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
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

func (s *service) callAnthropic(ctx context.Context, prepared *preparedChatRequest) (*ProxyResponse, string, error) {
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
	prepared.attemptDispatched = true
	response, err := s.httpClient.Do(httpRequest)
	if err != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("provider call failed: %v", err), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return nil, prepared.traceID, fmt.Errorf("failed to call provider: %w", err)
	}
	defer response.Body.Close()

	responseBody, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("failed to read provider response: %v", readErr), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
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
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, "provider returned non-success status: "+message, providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return proxyResponse, prepared.traceID, nil
	}

	var providerResponse anthropicMessageResponse
	if err := json.Unmarshal(responseBody, &providerResponse); err != nil {
		s.recordSuccess(prepared, providerRequestBody, string(responseBody), "", 0, 0, 0, int(time.Since(startedAt).Milliseconds()), false)
		return proxyResponse, prepared.traceID, nil
	}

	normalizedBody, assistantContent, promptTokens, completionTokens, totalTokens, normalizeErr := normalizeAnthropicResponse(providerResponse)
	if normalizeErr != nil {
		s.recordSuccess(prepared, providerRequestBody, string(responseBody), "", providerResponse.Usage.InputTokens, providerResponse.Usage.OutputTokens, providerResponse.Usage.InputTokens+providerResponse.Usage.OutputTokens, int(time.Since(startedAt).Milliseconds()), false)
		return proxyResponse, prepared.traceID, nil
	}

	proxyResponse.Headers.Del("Content-Length")
	proxyResponse.Body = normalizedBody
	s.recordSuccess(prepared, providerRequestBody, string(normalizedBody), assistantContent, promptTokens, completionTokens, totalTokens, int(time.Since(startedAt).Milliseconds()), false)
	return proxyResponse, prepared.traceID, nil
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
	requestPayload := visionSafePayload(providerRequestBody)
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
		InstanceMode: runtimeAttributionString(req.InstanceMode),
		RuntimeType:  runtimeAttributionString(req.RuntimeType),
		GatewayID:    runtimeAttributionString(req.GatewayID),
		RuntimePodID: runtimeAttributionInt64(req.RuntimePodID),
		GatewayKeyID: req.GatewayKeyID,
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
	requestPayload := visionSafePayload(providerRequestBody)
	if strings.TrimSpace(requestPayload) == "" {
		requestPayload = rawOrJSONRequestPayload(prepared.req)
	}
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
		TraceID:          prepared.traceID,
		SessionID:        prepared.req.SessionID,
		RequestID:        prepared.requestIDPtr,
		UserID:           prepared.userIDPtr,
		InstanceID:       prepared.req.InstanceID,
		InstanceMode:     runtimeAttributionString(prepared.req.InstanceMode),
		RuntimeType:      runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID:        runtimeAttributionString(prepared.req.GatewayID),
		RuntimePodID:     runtimeAttributionInt64(prepared.req.RuntimePodID),
		GatewayKeyID:     prepared.req.GatewayKeyID,
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

func setPartialAttemptUsage(prepared *preparedChatRequest, assistantContent string, promptTokens, completionTokens int) {
	if prepared == nil {
		return
	}
	if promptTokens <= 0 {
		promptTokens, _ = routingTokenEstimate(prepared.req)
	}
	if completionTokens <= 0 && strings.TrimSpace(assistantContent) != "" {
		completionTokens = estimateGatewayInputTokens(assistantContent)
	}
	prepared.attemptPromptTokens = promptTokens
	prepared.attemptCompletionTokens = completionTokens
}

func (s *service) streamOpenAICompatible(ctx context.Context, prepared *preparedChatRequest, w http.ResponseWriter, deferRetryableResponse bool) error {
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
	prepared.attemptDispatched = true
	response, err := s.streamHTTPClient.Do(httpRequest)
	if err != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("provider call failed: %v", err), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return fmt.Errorf("failed to call provider: %w", err)
	}
	defer response.Body.Close()
	prepared.attemptStatusCode = response.StatusCode

	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming response writer is not supported")
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(response.Body)
		prepared.attemptFailureResponse = &ProxyResponse{StatusCode: response.StatusCode, Body: responseBody}
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, "provider returned non-success status: "+message, providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		if deferRetryableResponse && isRetryableProviderResponse(prepared.attemptFailureResponse) {
			return &retryableStreamResponseError{StatusCode: response.StatusCode, Message: message}
		}
		copyProxyHeaders(w.Header(), response.Header)
		setGatewayModelHeaders(w.Header(), prepared)
		w.Header().Set("X-Trace-ID", prepared.traceID)
		w.WriteHeader(response.StatusCode)
		if len(responseBody) > 0 {
			_, _ = w.Write(responseBody)
		}
		flusher.Flush()
		return nil
	}

	copyProxyHeaders(w.Header(), response.Header)
	setGatewayModelHeaders(w.Header(), prepared)
	w.Header().Set("X-Trace-ID", prepared.traceID)
	// Do not commit the downstream 200 response until the first upstream stream
	// event is available. This preserves same-group failover when a provider
	// accepts a request but closes the body before producing any event.

	result, streamErr := proxyOpenAIStream(response.Body, w, flusher, deferRetryableResponse)
	if !result.FirstTokenAt.IsZero() {
		prepared.attemptTTFTMs = int(result.FirstTokenAt.Sub(startedAt).Milliseconds())
	}
	if streamErr != nil {
		setPartialAttemptUsage(prepared, result.AssistantText, result.PromptTokens, result.CompletionTokens)
		s.recordFailure(
			prepared.traceID,
			prepared.requestID,
			prepared.req,
			userIDOrZero(prepared.userIDPtr),
			prepared.resolvedModel,
			startedAt,
			streamErr.Error(),
			providerRequestBody,
			prepared.attemptNumber,
			prepared.attemptPromptTokens,
			prepared.attemptCompletionTokens,
		)
		var providerErr *upstreamStreamProviderError
		var readErr *upstreamStreamReadError
		if errors.As(streamErr, &readErr) &&
			!errors.As(streamErr, &providerErr) &&
			ctx.Err() == nil &&
			(!deferRetryableResponse || !result.FirstTokenAt.IsZero()) {
			if emitErr := emitOpenAIStreamError(w, flusher, prepared.traceID, streamErr); emitErr != nil {
				return errors.Join(streamErr, emitErr)
			}
		}
		return streamErr
	}

	assistantContent := result.AssistantText
	if strings.TrimSpace(assistantContent) == "" {
		assistantContent = result.RawStream
	}
	s.recordSuccess(
		prepared,
		providerRequestBody,
		result.RawStream,
		assistantContent,
		result.PromptTokens,
		result.CompletionTokens,
		result.TotalTokens,
		int(time.Since(startedAt).Milliseconds()),
		true,
	)
	return nil
}

func (s *service) streamAnthropic(ctx context.Context, prepared *preparedChatRequest, w http.ResponseWriter, deferRetryableResponse bool) error {
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
	prepared.attemptDispatched = true
	response, err := s.streamHTTPClient.Do(httpRequest)
	if err != nil {
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, fmt.Sprintf("provider call failed: %v", err), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return fmt.Errorf("failed to call provider: %w", err)
	}
	defer response.Body.Close()
	prepared.attemptStatusCode = response.StatusCode

	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming response writer is not supported")
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(response.Body)
		prepared.attemptFailureResponse = &ProxyResponse{StatusCode: response.StatusCode, Body: responseBody}
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, "provider returned non-success status: "+message, providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		if deferRetryableResponse && isRetryableProviderResponse(prepared.attemptFailureResponse) {
			return &retryableStreamResponseError{StatusCode: response.StatusCode, Message: message}
		}
		copyProxyHeaders(w.Header(), response.Header)
		setGatewayModelHeaders(w.Header(), prepared)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Trace-ID", prepared.traceID)
		w.WriteHeader(response.StatusCode)
		if len(responseBody) > 0 {
			_, _ = w.Write(responseBody)
		}
		flusher.Flush()
		return nil
	}

	copyProxyHeaders(w.Header(), response.Header)
	setGatewayModelHeaders(w.Header(), prepared)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Trace-ID", prepared.traceID)
	// As above, the first translated event commits the response. An empty or
	// broken upstream body can still fail over before the client sees headers.

	state := &anthropicStreamState{
		Created:   time.Now().Unix(),
		ToolCalls: map[int]*anthropicToolCallState{},
	}
	rawStream, streamErr := proxyAnthropicStream(response.Body, w, flusher, state, deferRetryableResponse)
	if !state.FirstTokenAt.IsZero() {
		prepared.attemptTTFTMs = int(state.FirstTokenAt.Sub(startedAt).Milliseconds())
	}
	if streamErr != nil {
		partialContent := state.AssistantText.String()
		if strings.TrimSpace(partialContent) == "" {
			partialContent = renderAnthropicToolCalls(state)
		}
		setPartialAttemptUsage(prepared, partialContent, state.PromptTokens, state.CompletionTokens)
		s.recordFailure(
			prepared.traceID,
			prepared.requestID,
			prepared.req,
			userIDOrZero(prepared.userIDPtr),
			prepared.resolvedModel,
			startedAt,
			streamErr.Error(),
			providerRequestBody,
			prepared.attemptNumber,
			prepared.attemptPromptTokens,
			prepared.attemptCompletionTokens,
		)
		if ctx.Err() == nil && (!deferRetryableResponse || !state.FirstTokenAt.IsZero()) {
			if emitErr := emitOpenAIStreamError(w, flusher, prepared.traceID, streamErr); emitErr != nil {
				return errors.Join(streamErr, emitErr)
			}
		}
		return streamErr
	}

	finalChunk := openAIStreamResponseChunk{
		ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []openAIStreamChoice{
			{
				Index: 0,
				Delta: openAIStreamDelta{},
			},
		},
		Usage: &struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     state.PromptTokens,
			CompletionTokens: state.CompletionTokens,
			TotalTokens:      state.PromptTokens + state.CompletionTokens,
		},
	}
	if err := emitOpenAIStreamPayload(w, flusher, finalChunk); err != nil {
		streamErr := &downstreamStreamWriteError{err: err}
		setPartialAttemptUsage(prepared, state.AssistantText.String(), state.PromptTokens, state.CompletionTokens)
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, streamErr.Error(), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return streamErr
	}
	if err := emitOpenAIStreamDone(w, flusher); err != nil {
		streamErr := &downstreamStreamWriteError{err: err}
		setPartialAttemptUsage(prepared, state.AssistantText.String(), state.PromptTokens, state.CompletionTokens)
		s.recordFailure(prepared.traceID, prepared.requestID, prepared.req, userIDOrZero(prepared.userIDPtr), prepared.resolvedModel, startedAt, streamErr.Error(), providerRequestBody, prepared.attemptNumber, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		return streamErr
	}

	assistantContent := strings.TrimSpace(state.AssistantText.String())
	if assistantContent == "" {
		assistantContent = renderAnthropicToolCalls(state)
	}
	if assistantContent == "" {
		assistantContent = rawStream
	}
	s.recordSuccess(prepared, providerRequestBody, rawStream, assistantContent, state.PromptTokens, state.CompletionTokens, state.PromptTokens+state.CompletionTokens, int(time.Since(startedAt).Milliseconds()), true)
	return nil
}

func proxyAnthropicStream(reader io.Reader, writer io.Writer, flusher http.Flusher, state *anthropicStreamState, deferUntilSemantic ...bool) (string, error) {
	buffered := bufio.NewReader(reader)
	var deferred *deferredStreamWriter
	if len(deferUntilSemantic) > 0 && deferUntilSemantic[0] {
		deferred = &deferredStreamWriter{writer: writer, flusher: flusher}
		writer, flusher = deferred, deferred
	}
	var rawStream strings.Builder
	eventType := ""
	dataLines := make([]string, 0, 2)
	done := false

	flushEvent := func() error {
		if len(dataLines) == 0 {
			eventType = ""
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		finished, err := processAnthropicStreamEvent(payload, eventType, state, writer, flusher)
		eventType = ""
		if err != nil {
			return err
		}
		if finished {
			done = true
		}
		if deferred != nil && (!state.FirstTokenAt.IsZero() || finished) {
			if err := deferred.Commit(); err != nil {
				return &downstreamStreamWriteError{err: err}
			}
		}
		return nil
	}

	for !done {
		line, readErr := buffered.ReadString('\n')
		if line != "" {
			rawStream.WriteString(line)
			trimmedLine := strings.TrimRight(line, "\r\n")
			switch {
			case trimmedLine == "":
				if err := flushEvent(); err != nil {
					return rawStream.String(), err
				}
			case strings.HasPrefix(trimmedLine, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(trimmedLine, "event:"))
			case strings.HasPrefix(trimmedLine, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:")))
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if err := flushEvent(); err != nil {
					return rawStream.String(), err
				}
				if done {
					return rawStream.String(), nil
				}
				return rawStream.String(), &upstreamStreamReadError{err: io.ErrUnexpectedEOF}
			}
			return rawStream.String(), &upstreamStreamReadError{err: readErr}
		}
	}
	return rawStream.String(), nil
}

func proxyOpenAIStream(reader io.Reader, writer io.Writer, flusher http.Flusher, deferUntilSemantic ...bool) (openAIStreamProxyResult, error) {
	buffered := bufio.NewReader(reader)
	var deferred *deferredStreamWriter
	if len(deferUntilSemantic) > 0 && deferUntilSemantic[0] {
		deferred = &deferredStreamWriter{writer: writer, flusher: flusher}
		writer, flusher = deferred, deferred
	}
	var rawStream strings.Builder
	var assistantText strings.Builder
	promptTokens := 0
	completionTokens := 0
	totalTokens := 0
	sawDone := false
	sawFinishReason := false
	var pendingProviderError error
	var firstTokenAt time.Time

	result := func() openAIStreamProxyResult {
		return openAIStreamProxyResult{
			RawStream:        rawStream.String(),
			AssistantText:    assistantText.String(),
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			SawDone:          sawDone,
			SawFinishReason:  sawFinishReason,
			Terminal:         sawDone || sawFinishReason,
			FirstTokenAt:     firstTokenAt,
		}
	}

	for {
		line, readErr := buffered.ReadString('\n')
		if line != "" {
			inspection := inspectStreamLine(
				line,
				&assistantText,
				&promptTokens,
				&completionTokens,
				&totalTokens,
			)
			sawDone = sawDone || inspection.Done
			sawFinishReason = sawFinishReason || inspection.Finished
			if inspection.Semantic && firstTokenAt.IsZero() {
				firstTokenAt = time.Now()
			}
			rawStream.WriteString(line)
			// A few OpenAI-compatible providers emit chain-of-thought deltas as
			// `delta.reasoning`. OpenCode and other clients generally consume the
			// standard `delta.reasoning_content` spelling instead. Keep the
			// provider payload in the audit stream unchanged, but add the standard
			// alias on the downstream stream so clients can finish their thinking
			// state and render the final content.
			downstreamLine := normalizeOpenAIReasoningDelta(line)
			// Commit BEFORE writing meaningful data: a single legitimate reasoning
			// chunk may itself exceed the bounded heartbeat/prelude buffer.
			if deferred != nil && pendingProviderError == nil && inspection.ProviderError == nil && (inspection.Semantic || inspection.Finished || inspection.Done) {
				if err := deferred.Commit(); err != nil {
					return result(), &downstreamStreamWriteError{err: err}
				}
			}
			if _, err := io.WriteString(writer, downstreamLine); err != nil {
				return result(), &downstreamStreamWriteError{err: err}
			}
			flusher.Flush()
			if inspection.ProviderError != nil {
				pendingProviderError = inspection.ProviderError
			}
			if pendingProviderError != nil && strings.TrimSpace(line) == "" {
				return result(), pendingProviderError
			}
			if inspection.Done {
				return result(), nil
			}
		}

		if readErr != nil {
			if pendingProviderError != nil {
				return result(), pendingProviderError
			}
			if errors.Is(readErr, io.EOF) {
				if sawFinishReason {
					return result(), nil
				}
				return result(), &upstreamStreamReadError{err: io.ErrUnexpectedEOF}
			}
			return result(), &upstreamStreamReadError{err: readErr}
		}
	}
}

// normalizeOpenAIReasoningDelta adds the OpenAI-compatible reasoning_content
// alias to provider chunks that only expose reasoning. Invalid/non-data SSE
// lines and chunks that already contain reasoning_content are returned byte
// for byte unchanged.
func normalizeOpenAIReasoningDelta(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return line
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" || !strings.HasPrefix(payload, "{") {
		return line
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return line
	}
	choices, ok := envelope["choices"].([]interface{})
	if !ok {
		return line
	}
	changed := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if existing := delta["reasoning_content"]; existing != nil && existing != "" {
			continue
		}
		if reasoning, ok := delta["reasoning"].(string); ok && reasoning != "" {
			delta["reasoning_content"] = reasoning
			changed = true
		}
	}
	if !changed {
		return line
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return line
	}
	// Preserve the SSE line ending style expected by the caller.
	ending := ""
	if strings.HasSuffix(line, "\r\n") {
		ending = "\r\n"
	} else if strings.HasSuffix(line, "\n") {
		ending = "\n"
	}
	return "data: " + string(encoded) + ending
}

func inspectStreamLine(line string, assistantText *strings.Builder, promptTokens, completionTokens, totalTokens *int) openAIStreamInspection {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return openAIStreamInspection{}
	}

	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" {
		return openAIStreamInspection{}
	}
	if payload == "[DONE]" {
		return openAIStreamInspection{Done: true}
	}

	var chunk openAIStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return openAIStreamInspection{}
	}
	if chunk.Error != nil {
		message := strings.TrimSpace(chunk.Error.Message)
		if message == "" {
			message = "provider returned an error stream event"
		}
		return openAIStreamInspection{
			ProviderError: &upstreamStreamProviderError{message: message},
		}
	}

	if chunk.Usage != nil {
		*promptTokens = chunk.Usage.PromptTokens
		*completionTokens = chunk.Usage.CompletionTokens
		*totalTokens = chunk.Usage.TotalTokens
	}

	finished := false
	semantic := false
	for _, choice := range chunk.Choices {
		content := flattenMessageContent(choice.Delta.Content)
		if content != "" {
			assistantText.WriteString(content)
			semantic = true
		}
		if len(choice.Delta.ToolCalls) > 0 {
			semantic = true
		}
		// Reasoning is visible progress, not a heartbeat. Do not append it to
		// AssistantText: that field represents the final answer, not thinking.
		for _, value := range []interface{}{choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.Refusal} {
			if text, ok := value.(string); ok && text != "" {
				semantic = true
			}
		}
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			finished = true
		}
	}
	return openAIStreamInspection{Finished: finished, Semantic: semantic}
}

func buildProviderRequestBody(req ChatCompletionRequest, model *models.LLMModel) ([]byte, error) {
	if model == nil {
		return nil, errors.New("model is not active or does not exist")
	}

	switch models.ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType) {
	case models.ProtocolTypeAnthropic:
		return buildAnthropicRequestBody(req, model)
	default:
		return buildOpenAICompatibleRequestBody(req, model)
	}
}

func processAnthropicStreamEvent(payload, eventType string, state *anthropicStreamState, w io.Writer, flusher http.Flusher) (bool, error) {
	if strings.TrimSpace(payload) == "" {
		return false, nil
	}

	var event anthropicStreamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return false, nil
	}
	if strings.TrimSpace(eventType) == "" {
		eventType = event.Type
	}

	switch eventType {
	case "message_start":
		if event.Message != nil {
			state.ResponseID = event.Message.ID
			state.Model = event.Message.Model
			state.PromptTokens = event.Message.Usage.InputTokens
		}
	case "content_block_start":
		if event.ContentBlock == nil || event.Index == nil {
			return false, nil
		}
		index := *event.Index
		switch event.ContentBlock.Type {
		case "text":
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							Role: conditionalAssistantRole(state),
						},
					},
				},
			}
			if err := emitOpenAIStreamPayload(w, flusher, chunk); err != nil {
				return false, err
			}
		case "tool_use":
			if state.FirstTokenAt.IsZero() {
				state.FirstTokenAt = time.Now()
			}
			toolState := &anthropicToolCallState{
				ID:   defaultIfBlank(event.ContentBlock.ID, fmt.Sprintf("call_%d", index)),
				Name: strings.TrimSpace(event.ContentBlock.Name),
			}
			state.ToolCalls[index] = toolState
			toolCallIndex := index
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							Role: conditionalAssistantRole(state),
							ToolCalls: []ToolCall{
								{
									ID:    toolState.ID,
									Type:  "function",
									Index: &toolCallIndex,
									Function: &ToolCallFunction{
										Name:      toolState.Name,
										Arguments: "",
									},
								},
							},
						},
					},
				},
			}
			if err := emitOpenAIStreamPayload(w, flusher, chunk); err != nil {
				return false, err
			}
		}
	case "content_block_delta":
		if event.Index == nil || event.Delta == nil {
			return false, nil
		}
		index := *event.Index
		switch event.Delta.Type {
		case "text_delta":
			if event.Delta.Text == "" {
				return false, nil
			}
			state.AssistantText.WriteString(event.Delta.Text)
			if state.FirstTokenAt.IsZero() {
				state.FirstTokenAt = time.Now()
			}
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							Content: event.Delta.Text,
						},
					},
				},
			}
			if err := emitOpenAIStreamPayload(w, flusher, chunk); err != nil {
				return false, err
			}
		case "input_json_delta":
			toolState := state.ToolCalls[index]
			if toolState == nil {
				return false, nil
			}
			toolState.Arguments.WriteString(event.Delta.PartialJSON)
			toolCallIndex := index
			chunk := openAIStreamResponseChunk{
				ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []openAIStreamChoice{
					{
						Index: 0,
						Delta: openAIStreamDelta{
							ToolCalls: []ToolCall{
								{
									ID:    toolState.ID,
									Type:  "function",
									Index: &toolCallIndex,
									Function: &ToolCallFunction{
										Arguments: event.Delta.PartialJSON,
									},
								},
							},
						},
					},
				},
			}
			if err := emitOpenAIStreamPayload(w, flusher, chunk); err != nil {
				return false, err
			}
		}
	case "message_delta":
		if event.Usage != nil {
			state.CompletionTokens = event.Usage.OutputTokens
		}
		if event.Delta != nil {
			finishReason := normalizeAnthropicStopReason(event.Delta.StopReason)
			if finishReason != "" {
				chunk := openAIStreamResponseChunk{
					ID:      defaultIfBlank(state.ResponseID, "chatcmpl-anthropic"),
					Object:  "chat.completion.chunk",
					Created: state.Created,
					Model:   state.Model,
					Choices: []openAIStreamChoice{
						{
							Index:        0,
							Delta:        openAIStreamDelta{},
							FinishReason: &finishReason,
						},
					},
				}
				if err := emitOpenAIStreamPayload(w, flusher, chunk); err != nil {
					return false, err
				}
			}
		}
	case "message_stop":
		return true, nil
	case "error":
		if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
			return false, errors.New(event.Error.Message)
		}
	}

	return false, nil
}

func emitOpenAIStreamPayload(w io.Writer, flusher http.Flusher, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func emitOpenAIStreamDone(w io.Writer, flusher http.Flusher) error {
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func emitOpenAIStreamError(w io.Writer, flusher http.Flusher, traceID string, _ error) error {
	payload := struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			TraceID string `json:"trace_id,omitempty"`
		} `json:"error"`
	}{}
	payload.Error.Message = upstreamStreamInterruptedCode + ": upstream model stream ended before a verified terminal event; retry this turn"
	payload.Error.Type = "upstream_stream_error"
	payload.Error.Code = upstreamStreamInterruptedCode
	payload.Error.TraceID = strings.TrimSpace(traceID)
	return emitOpenAIStreamPayload(w, flusher, payload)
}

func conditionalAssistantRole(state *anthropicStreamState) string {
	if state.SentRole {
		return ""
	}
	state.SentRole = true
	return "assistant"
}

func buildOpenAICompatibleRequestBody(req ChatCompletionRequest, model *models.LLMModel) ([]byte, error) {
	if err := NormalizeGenerationLimits(&req); err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("model is not active or does not exist")
	}
	payload := map[string]json.RawMessage{}
	if len(req.RawBody) > 0 {
		if err := json.Unmarshal(req.RawBody, &payload); err != nil {
			return nil, fmt.Errorf("failed to decode provider request: %w", err)
		}
	} else {
		// HTTP proxy calls retain RawBody so unknown provider extensions survive.
		// Internal callers construct ChatCompletionRequest directly, so synthesize
		// the equivalent wire payload instead of silently dropping messages and
		// generation parameters.
		structuredBody, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to encode structured provider request: %w", err)
		}
		if err := json.Unmarshal(structuredBody, &payload); err != nil {
			return nil, fmt.Errorf("failed to decode structured provider request: %w", err)
		}
	}
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}
	delete(payload, "session_id")
	delete(payload, "instance_id")
	delete(payload, "instance_mode")
	delete(payload, "runtime_type")
	delete(payload, "gateway_id")
	delete(payload, "runtime_pod_id")
	delete(payload, "trace_id")
	delete(payload, "request_id")
	modelPayload, err := json.Marshal(model.ProviderModelName)
	if err != nil {
		return nil, fmt.Errorf("failed to encode provider model name: %w", err)
	}
	payload["model"] = json.RawMessage(modelPayload)
	limit, _ := json.Marshal(*req.MaxTokens)
	if req.MaxCompletionTokens != nil {
		payload["max_completion_tokens"] = limit
		delete(payload, "max_tokens")
	} else {
		payload["max_tokens"] = limit
		delete(payload, "max_completion_tokens")
	}
	if err := applyOpenAICompatibleProviderContract(payload, model, req.ManagedAgentType); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode provider request: %w", err)
	}
	return body, nil
}

func applyOpenAICompatibleProviderContract(payload map[string]json.RawMessage, model *models.LLMModel, managedAgentType *string) error {
	if payload == nil || model == nil {
		return nil
	}
	if models.EffectiveLLMReasoningControl(model) == models.ReasoningControlDeepSeekThinking {
		// DeepSeek's OpenAI-compatible API accepts max_tokens. OpenClaw uses
		// max_completion_tokens for an opaque ClawManager proxy because it cannot
		// see the resolved downstream provider. Normalize only after ClawManager
		// has resolved the official DeepSeek route; arbitrary compatible proxies
		// retain their original wire contract.
		if _, hasMaxTokens := payload["max_tokens"]; !hasMaxTokens {
			if value, hasMaxCompletionTokens := payload["max_completion_tokens"]; hasMaxCompletionTokens {
				payload["max_tokens"] = value
			}
		}
		delete(payload, "max_completion_tokens")
	}
	return applyReasoningControl(payload, model, managedAgentType)
}

type reasoningRequestIntent struct {
	set     bool
	enabled bool
	effort  string
}

func openAICompatibleReasoningIntent(payload map[string]json.RawMessage) reasoningRequestIntent {
	if payload == nil {
		return reasoningRequestIntent{}
	}
	if raw, ok := payload["enable_thinking"]; ok {
		var enabled *bool
		if json.Unmarshal(raw, &enabled) == nil && enabled != nil {
			return reasoningRequestIntent{set: true, enabled: *enabled}
		}
	}
	if raw, ok := payload["chat_template_kwargs"]; ok {
		var options struct {
			EnableThinking *bool `json:"enable_thinking"`
		}
		if json.Unmarshal(raw, &options) == nil && options.EnableThinking != nil {
			return reasoningRequestIntent{set: true, enabled: *options.EnableThinking}
		}
	}
	if raw, ok := payload["thinking"]; ok {
		var thinking struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &thinking) == nil {
			switch strings.ToLower(strings.TrimSpace(thinking.Type)) {
			case "disabled", "off", "none":
				return reasoningRequestIntent{set: true}
			case "enabled", "on", "adaptive":
				return reasoningRequestIntent{set: true, enabled: true}
			}
		}
	}
	if raw, ok := payload["reasoning_effort"]; ok {
		var effort string
		if json.Unmarshal(raw, &effort) == nil {
			effort = strings.ToLower(strings.TrimSpace(effort))
			switch effort {
			case "off", "none", "disabled":
				return reasoningRequestIntent{set: true}
			case "minimal", "low", "medium", "high", "xhigh", "max":
				return reasoningRequestIntent{set: true, enabled: true, effort: effort}
			}
		}
	}
	if raw, ok := payload["reasoning"]; ok {
		var reasoning struct {
			Effort json.RawMessage `json:"effort"`
		}
		if json.Unmarshal(raw, &reasoning) == nil && len(reasoning.Effort) > 0 {
			return openAICompatibleReasoningIntent(map[string]json.RawMessage{"reasoning_effort": reasoning.Effort})
		}
	}
	return reasoningRequestIntent{}
}

func normalizeDeepSeekReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "xhigh", "max":
		return "max"
	case "minimal", "low", "medium", "high":
		return "high"
	default:
		return ""
	}
}

func isManagedOpenClawRequest(managedAgentType *string) bool {
	return managedAgentType != nil && strings.EqualFold(strings.TrimSpace(*managedAgentType), "openclaw")
}

func applyReasoningControl(payload map[string]json.RawMessage, model *models.LLMModel, managedAgentType *string) error {
	if payload == nil || model == nil {
		return nil
	}
	control := models.EffectiveLLMReasoningControl(model)
	if control == models.ReasoningControlThinkingType || control == models.ReasoningControlEnableThinking || control == models.ReasoningControlChatTemplate {
		return applyConfiguredReasoningControl(payload, model, managedAgentType, control)
	}
	switch control {
	case models.ReasoningControlDeepSeekThinking:
		// The model setting is the capability ceiling. Managed OpenClaw requests
		// may further disable thinking for the current session/turn, but may never
		// enable a model that the administrator disabled.
		intent := combinedReasoningIntent(payload)
		// Explicit DeepSeek adapters may receive switches intended for another
		// provider. Remove those before writing the selected wire contract.
		if err := clearAlternativeThinkingSwitches(payload); err != nil {
			return err
		}
		enabled := model.ReasoningEnabled
		if isManagedOpenClawRequest(managedAgentType) {
			enabled = enabled && intent.set && intent.enabled
		} else if intent.set {
			enabled = enabled && intent.enabled
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		} else {
			delete(payload, "reasoning_effort")
			delete(payload, "reasoning")
		}
		encoded, err := json.Marshal(map[string]string{"type": state})
		if err != nil {
			return fmt.Errorf("failed to encode provider reasoning control: %w", err)
		}
		payload["thinking"] = encoded
		if enabled {
			if effort := normalizeDeepSeekReasoningEffort(intent.effort); effort != "" {
				encodedEffort, err := json.Marshal(effort)
				if err != nil {
					return fmt.Errorf("failed to encode provider reasoning effort: %w", err)
				}
				payload["reasoning_effort"] = encodedEffort
			}
		}
	}
	return nil
}

func buildAnthropicRequestBody(req ChatCompletionRequest, model *models.LLMModel) ([]byte, error) {
	if err := NormalizeVisionRequest(&req); err != nil {
		return nil, err
	}
	if !visionTransportSupported(models.ProtocolTypeAnthropic, req) {
		return nil, invalidImage("Anthropic cannot preserve this image role or explicit detail option")
	}
	if err := NormalizeGenerationLimits(&req); err != nil {
		return nil, err
	}
	systemPrompt, messages := convertChatMessagesToAnthropic(req.Messages)
	tools, err := convertToolsToAnthropic(req.Tools)
	if err != nil {
		return nil, err
	}
	toolChoice, err := convertToolChoiceToAnthropic(req.ToolChoice)
	if err != nil {
		return nil, err
	}

	stopSequences, err := convertStopSequences(req.Stop)
	if err != nil {
		return nil, err
	}

	maxTokens := defaultAnthropicMaxTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	payload := anthropicRequestPayload{
		Model:       model.ProviderModelName,
		Messages:    messages,
		System:      systemPrompt,
		MaxTokens:   maxTokens,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		StopSeqs:    stopSequences,
	}
	if len(tools) > 0 {
		payload.Tools = tools
		if toolChoice != nil {
			payload.ToolChoice = toolChoice
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode anthropic request: %w", err)
	}
	return body, nil
}

func buildProviderHTTPRequest(ctx context.Context, traceID, requestID string, model *models.LLMModel, providerRequestBody []byte, resolvedAPIKey *string, acceptStream bool) (*http.Request, error) {
	if model == nil {
		return nil, errors.New("model is not active or does not exist")
	}

	switch models.ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType) {
	case models.ProtocolTypeAnthropic:
		return buildAnthropicProviderHTTPRequest(ctx, traceID, requestID, model, providerRequestBody, resolvedAPIKey, acceptStream)
	default:
		return buildOpenAICompatibleProviderHTTPRequest(ctx, traceID, requestID, model, providerRequestBody, resolvedAPIKey, acceptStream)
	}
}

func buildOpenAICompatibleProviderHTTPRequest(ctx context.Context, traceID, requestID string, model *models.LLMModel, providerRequestBody []byte, resolvedAPIKey *string, acceptStream bool) (*http.Request, error) {
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

func buildAnthropicProviderHTTPRequest(ctx context.Context, traceID, requestID string, model *models.LLMModel, providerRequestBody []byte, resolvedAPIKey *string, acceptStream bool) (*http.Request, error) {
	endpoint, err := buildProviderAPIEndpoint(model.BaseURL, "v1", "messages")
	if err != nil {
		return nil, err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(providerRequestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to build provider request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("anthropic-version", anthropicVersionHeader)
	if acceptStream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	} else {
		httpRequest.Header.Set("Accept", "application/json")
	}
	httpRequest.Header.Set("X-Trace-ID", traceID)
	httpRequest.Header.Set("X-Request-ID", requestID)
	if resolvedAPIKey != nil && strings.TrimSpace(*resolvedAPIKey) != "" {
		httpRequest.Header.Set("x-api-key", strings.TrimSpace(*resolvedAPIKey))
	}
	return httpRequest, nil
}

func buildProviderAPIEndpoint(baseURL, versionPrefix, resource string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if trimmed == "" {
		return "", errors.New("base URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("base URL is invalid")
	}

	versionPath := "/" + strings.Trim(versionPrefix, "/")
	resourcePath := strings.Trim(resource, "/")
	if strings.HasSuffix(strings.ToLower(parsed.Path), strings.ToLower(versionPath)) {
		return trimmed + "/" + resourcePath, nil
	}

	pathSegments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	lastSegment := ""
	if len(pathSegments) > 0 {
		lastSegment = pathSegments[len(pathSegments)-1]
	}
	if providerVersionSegmentPattern.MatchString(lastSegment) {
		return trimmed + "/" + resourcePath, nil
	}

	return trimmed + versionPath + "/" + resourcePath, nil
}

func convertChatMessagesToAnthropic(messages []ChatMessage) (string, []anthropicRequestMessage) {
	systemParts := make([]string, 0)
	converted := make([]anthropicRequestMessage, 0, len(messages))

	appendMessage := func(role string, blocks []anthropicContentBlock) {
		if len(blocks) == 0 {
			return
		}
		if len(converted) > 0 && converted[len(converted)-1].Role == role {
			converted[len(converted)-1].Content = append(converted[len(converted)-1].Content, blocks...)
			return
		}
		converted = append(converted, anthropicRequestMessage{
			Role:    role,
			Content: blocks,
		})
	}

	for _, message := range messages {
		role := strings.TrimSpace(strings.ToLower(message.Role))
		switch role {
		case "system":
			if text := strings.TrimSpace(flattenChatMessage(message)); text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			appendMessage("assistant", convertAssistantMessageToAnthropicBlocks(message))
		case "tool":
			appendMessage("user", convertToolMessageToAnthropicBlocks(message))
		default:
			appendMessage("user", convertUserMessageToAnthropicBlocks(message))
		}
	}

	return strings.Join(systemParts, "\n\n"), converted
}

func convertUserMessageToAnthropicBlocks(message ChatMessage) []anthropicContentBlock {
	blocks := convertContentToAnthropicTextBlocks(message.Content)
	if len(blocks) > 0 {
		return blocks
	}
	fallback := strings.TrimSpace(flattenChatMessage(message))
	if fallback == "" {
		return nil
	}
	return []anthropicContentBlock{{Type: "text", Text: fallback}}
}

func convertAssistantMessageToAnthropicBlocks(message ChatMessage) []anthropicContentBlock {
	blocks := convertContentToAnthropicTextBlocks(message.Content)
	for _, toolCall := range message.ToolCalls {
		if toolCall.Function == nil {
			continue
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    defaultIfBlank(toolCall.ID, "tool_call"),
			Name:  strings.TrimSpace(toolCall.Function.Name),
			Input: parseToolCallArguments(toolCall.Function.Arguments),
		})
	}
	return blocks
}

func convertToolMessageToAnthropicBlocks(message ChatMessage) []anthropicContentBlock {
	toolUseID := strings.TrimSpace(message.ToolCallID)
	if imageCount(message.Content) > 0 {
		return []anthropicContentBlock{{Type: "tool_result", ToolUseID: toolUseID, Content: convertContentToAnthropicTextBlocks(message.Content)}}
	}
	content := strings.TrimSpace(flattenMessageContent(message.Content))
	if content == "" {
		content = mustJSONString(message.Content)
	}
	if toolUseID == "" && content == "" {
		return nil
	}
	return []anthropicContentBlock{
		{
			Type:      "tool_result",
			ToolUseID: toolUseID,
			Content:   defaultIfBlank(content, "{}"),
		},
	}
}

func convertContentToAnthropicTextBlocks(content interface{}) []anthropicContentBlock {
	if blocks, ok := visionAnthropicBlocks(content); ok {
		return blocks
	}
	switch value := content.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []anthropicContentBlock{{Type: "text", Text: value}}
	case []interface{}:
		blocks := make([]anthropicContentBlock, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(flattenStructuredContentPart(item))
			if text == "" {
				continue
			}
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: text})
		}
		if len(blocks) > 0 {
			return blocks
		}
	case map[string]interface{}:
		if text := strings.TrimSpace(flattenStructuredContentPart(value)); text != "" {
			return []anthropicContentBlock{{Type: "text", Text: text}}
		}
	}

	flattened := strings.TrimSpace(flattenMessageContent(content))
	if flattened == "" {
		return nil
	}
	return []anthropicContentBlock{{Type: "text", Text: flattened}}
}

func convertToolsToAnthropic(raw json.RawMessage) ([]anthropicToolDefinition, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("failed to decode provider tools: %w", err)
	}

	tools := make([]anthropicToolDefinition, 0, len(items))
	for _, item := range items {
		itemType, _ := item["type"].(string)
		if strings.TrimSpace(itemType) != "" && !strings.EqualFold(strings.TrimSpace(itemType), "function") {
			continue
		}
		functionValue, ok := item["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := functionValue["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		description, _ := functionValue["description"].(string)
		inputSchema := functionValue["parameters"]
		if inputSchema == nil {
			inputSchema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		tools = append(tools, anthropicToolDefinition{
			Name:        strings.TrimSpace(name),
			Description: strings.TrimSpace(description),
			InputSchema: inputSchema,
		})
	}

	return tools, nil
}

func convertToolChoiceToAnthropic(raw json.RawMessage) (map[string]interface{}, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var parsed interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode tool choice: %w", err)
	}

	switch value := parsed.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "none":
			return nil, nil
		case "required":
			return map[string]interface{}{"type": "any"}, nil
		case "auto", "any":
			return map[string]interface{}{"type": strings.ToLower(strings.TrimSpace(value))}, nil
		}
	case map[string]interface{}:
		choiceType, _ := value["type"].(string)
		switch strings.ToLower(strings.TrimSpace(choiceType)) {
		case "none", "":
			return nil, nil
		case "function":
			functionValue, _ := value["function"].(map[string]interface{})
			name, _ := functionValue["name"].(string)
			if strings.TrimSpace(name) == "" {
				return nil, nil
			}
			return map[string]interface{}{"type": "tool", "name": strings.TrimSpace(name)}, nil
		case "tool":
			return value, nil
		case "auto", "any":
			return map[string]interface{}{"type": strings.ToLower(strings.TrimSpace(choiceType))}, nil
		}
	}

	return nil, nil
}

func convertStopSequences(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			return nil, nil
		}
		return []string{single}, nil
	}

	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("failed to decode stop sequences: %w", err)
	}

	filtered := make([]string, 0, len(many))
	for _, item := range many {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered, nil
}

func parseToolCallArguments(raw string) interface{} {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]interface{}{}
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return map[string]interface{}{
			"raw": trimmed,
		}
	}
	return parsed
}

func normalizeAnthropicResponse(response anthropicMessageResponse) ([]byte, string, int, int, int, error) {
	normalized := ChatCompletionResponse{
		ID:      defaultIfBlank(response.ID, "chatcmpl-anthropic"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   response.Model,
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     response.Usage.InputTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.InputTokens + response.Usage.OutputTokens,
		},
	}

	contentValue, toolCalls := convertAnthropicBlocksToOpenAIMessage(response.Content)
	normalized.Choices = []struct {
		Index   int `json:"index"`
		Message struct {
			Role      string      `json:"role"`
			Content   interface{} `json:"content"`
			ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
			Refusal   interface{} `json:"refusal,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason,omitempty"`
	}{
		{
			Index: 0,
			Message: struct {
				Role      string      `json:"role"`
				Content   interface{} `json:"content"`
				ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
				Refusal   interface{} `json:"refusal,omitempty"`
			}{
				Role:      defaultIfBlank(response.Role, "assistant"),
				Content:   contentValue,
				ToolCalls: toolCalls,
			},
			FinishReason: normalizeAnthropicStopReason(response.StopReason),
		},
	}

	body, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", 0, 0, 0, err
	}

	return body, extractAssistantContent(normalized), normalized.Usage.PromptTokens, normalized.Usage.CompletionTokens, normalized.Usage.TotalTokens, nil
}

func convertAnthropicBlocksToOpenAIMessage(blocks []anthropicContentBlock) (interface{}, []ToolCall) {
	textParts := make([]string, 0)
	toolCalls := make([]ToolCall, 0)
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			inputPayload := "{}"
			if block.Input != nil {
				if encoded, err := json.Marshal(block.Input); err == nil {
					inputPayload = string(encoded)
				}
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   defaultIfBlank(block.ID, "tool_call"),
				Type: "function",
				Function: &ToolCallFunction{
					Name:      strings.TrimSpace(block.Name),
					Arguments: inputPayload,
				},
			})
		}
	}

	if len(textParts) > 0 {
		return strings.Join(textParts, "\n"), toolCalls
	}
	if len(toolCalls) > 0 {
		return nil, toolCalls
	}
	return "", nil
}

func normalizeAnthropicStopReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return ""
	}
}

func renderAnthropicToolCalls(state *anthropicStreamState) string {
	if len(state.ToolCalls) == 0 {
		return ""
	}

	indexes := make([]int, 0, len(state.ToolCalls))
	for index := range state.ToolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	parts := make([]string, 0, len(state.ToolCalls))
	for _, index := range indexes {
		toolState := state.ToolCalls[index]
		if toolState == nil {
			continue
		}
		args := strings.TrimSpace(toolState.Arguments.String())
		switch {
		case toolState.Name != "" && args != "":
			parts = append(parts, fmt.Sprintf("tool_call %s(%s)", toolState.Name, args))
		case toolState.Name != "":
			parts = append(parts, "tool_call "+toolState.Name)
		}
	}
	return strings.Join(parts, "\n")
}

func defaultIfBlank(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *service) recordBlockedInvocation(traceID, requestID string, req ChatCompletionRequest, userID int, model *models.LLMModel, reason string) *int {
	var modelID *int
	if model != nil {
		modelID = intPtr(model.ID)
	} else {
		model = &models.LLMModel{}
	}
	requestPayload := visionSafePayload([]byte(mustJSONString(req)))
	responsePayload := reason
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
		ModelID:             modelID,
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

func (s *service) resolveTargetModel(selectedModel *models.LLMModel, analysis services.RiskAnalysis, allowedGroups ...string) (*models.LLMModel, string, error) {
	return s.resolveTargetModelForRequest(selectedModel, analysis, ChatCompletionRequest{AllowedModelGroups: allowedGroups})
}

func (s *service) resolveTargetModelForRequest(selectedModel *models.LLMModel, analysis services.RiskAnalysis, req ChatCompletionRequest) (*models.LLMModel, string, error) {
	if selectedModel == nil {
		return nil, models.RiskActionAllow, errors.New("model is not active or does not exist")
	}
	if !analysis.IsSensitive {
		return selectedModel, models.RiskActionAllow, nil
	}
	if analysis.HighestAction == "require_approval" || analysis.HighestAction == models.RiskActionBlock {
		return nil, models.RiskActionBlock, errors.New("request was blocked by risk policy")
	}
	if analysis.HighestAction != models.RiskActionRouteSecureModel {
		return selectedModel, models.RiskActionAllow, nil
	}
	if selectedModel.IsSecure {
		return selectedModel, models.RiskActionRouteSecureModel, nil
	}

	activeModels, err := s.listActiveModels()
	if err != nil {
		return nil, models.RiskActionBlock, fmt.Errorf("failed to list active secure models: %w", err)
	}
	for _, item := range activeModels {
		if item.IsSecure && modelAllowedForGatewayKey(&item, req.AllowedModelGroups) && modelTransportSupported(&item, req) && (req.BudgetController == nil || routingCurrency(&item) == services.GatewayBudgetCurrency) {
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
	requestedModel = strings.TrimSpace(requestedModel)
	// Preserve the scoped repository's explicit denial (including its stable
	// error code) before attempting logical-group aliases.
	configured, lookupErr := s.modelRepo.GetByDisplayName(requestedModel)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if configured != nil && configured.IsActive {
		if !models.LLMChatAvailable(configured) {
			return nil, models.ErrChatNotSupported
		}
		return configured, nil
	}
	items, err := s.listActiveModels()
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	for _, item := range items {
		if strings.TrimSpace(item.DisplayName) == requestedModel {
			selected := item
			if !models.LLMChatAvailable(&selected) {
				return nil, models.ErrChatNotSupported
			}
			return &selected, nil
		}
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.DisplayName), requestedModel) {
			selected := item
			if !models.LLMChatAvailable(&selected) {
				return nil, models.ErrChatNotSupported
			}
			return &selected, nil
		}
	}
	return s.selectModelGroup(requestedModel)
}

func (s *service) selectAutoModel() (*models.LLMModel, error) {
	items, err := s.listActiveModels()
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	items = models.ChatModels(items)
	if len(items) == 0 {
		return nil, errors.New("no active models are configured")
	}

	for _, item := range orderModelDeployments(items) {
		if !item.IsSecure {
			selected := item
			return &selected, nil
		}
	}

	selected := items[0]
	return &selected, nil
}

func (s *service) selectAutoModelFromGroups(allowedGroups []string) (*models.LLMModel, error) {
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	for _, item := range orderModelDeployments(models.ChatModels(items)) {
		if !item.IsSecure && modelAllowedForGatewayKey(&item, allowedGroups) {
			selected := item
			return &selected, nil
		}
	}
	for _, item := range orderModelDeployments(models.ChatModels(items)) {
		if modelAllowedForGatewayKey(&item, allowedGroups) {
			selected := item
			return &selected, nil
		}
	}
	return nil, errors.New("gateway key has no active allowed model groups")
}

func modelAllowedForGatewayKey(model *models.LLMModel, allowedGroups []string) bool {
	if model == nil || len(allowedGroups) == 0 {
		return true
	}
	group := strings.TrimSpace(model.ModelGroup)
	if group == "" {
		group = strings.TrimSpace(model.DisplayName)
	}
	for _, allowed := range allowedGroups {
		if strings.EqualFold(group, strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}

// selectModelGroup resolves a stable logical model name to its preferred
// active deployment. This is intentionally compatible with legacy display
// names: an exact display-name match always wins before group lookup.
func (s *service) selectModelGroup(modelGroup string) (*models.LLMModel, error) {
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	modelGroup = strings.TrimSpace(modelGroup)
	for _, item := range orderModelDeployments(models.ChatModels(items)) {
		if strings.EqualFold(strings.TrimSpace(item.ModelGroup), modelGroup) {
			selected := item
			return &selected, nil
		}
	}
	return nil, errors.New("model is not active or does not exist")
}

func orderModelDeployments(items []models.LLMModel) []models.LLMModel {
	ordered := append([]models.LLMModel(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].DeploymentPriority != ordered[j].DeploymentPriority {
			return ordered[i].DeploymentPriority < ordered[j].DeploymentPriority
		}
		return strings.Compare(ordered[i].DisplayName, ordered[j].DisplayName) < 0
	})
	return ordered
}

func (s *service) deploymentCandidates(selected *models.LLMModel) []*models.LLMModel {
	if !models.LLMChatAvailable(selected) {
		return nil
	}
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return []*models.LLMModel{selected}
	}
	group := strings.TrimSpace(selected.ModelGroup)
	if group == "" {
		group = selected.DisplayName
	}
	candidates := make([]*models.LLMModel, 0)
	for _, item := range orderModelDeployments(items) {
		itemGroup := strings.TrimSpace(item.ModelGroup)
		if itemGroup == "" {
			itemGroup = item.DisplayName
		}
		// A secure reroute may only fail over to other secure deployments.
		if models.LLMChatAvailable(&item) && strings.EqualFold(itemGroup, group) && item.IsSecure == selected.IsSecure {
			candidate := item
			candidates = append(candidates, &candidate)
		}
	}
	if len(candidates) == 0 {
		return []*models.LLMModel{selected}
	}
	return candidates
}

func prioritizeSelectedDeployment(candidates []*models.LLMModel, selected *models.LLMModel) []*models.LLMModel {
	if selected == nil || len(candidates) < 2 {
		return candidates
	}
	ordered := make([]*models.LLMModel, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && candidate.ID == selected.ID {
			ordered = append(ordered, candidate)
			break
		}
	}
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == selected.ID {
			continue
		}
		ordered = append(ordered, candidate)
	}
	if len(ordered) == 0 {
		return candidates
	}
	return ordered
}

func isAutoModelRequest(requestedModel string) bool {
	return strings.EqualFold(strings.TrimSpace(requestedModel), autoModelID)
}

func calculateEstimatedCost(model *models.LLMModel, promptTokens, completionTokens int) float64 {
	return (float64(promptTokens) * model.InputPrice / 1_000_000.0) + (float64(completionTokens) * model.OutputPrice / 1_000_000.0)
}

func estimateGatewayBudgetCost(req ChatCompletionRequest, candidates []*models.LLMModel) float64 {
	promptTokens, completionTokens := routingTokenEstimate(req)
	inputCost := 0.0
	maxOutputCost := 0.0
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		// A retry can submit the prompt to every candidate, while a full output
		// should only be produced by the terminal successful deployment.
		inputCost += float64(promptTokens) * candidate.InputPrice / 1_000_000.0
		if cost := float64(completionTokens) * candidate.OutputPrice / 1_000_000.0; cost > maxOutputCost {
			maxOutputCost = cost
		}
	}
	return inputCost + maxOutputCost
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

func runtimeAttributionString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func runtimeAttributionInt64(value *int64) *int64 {
	if value == nil || *value <= 0 {
		return nil
	}
	copied := *value
	return &copied
}

func applyInvocationRuntimeAttribution(invocation *models.ModelInvocation, req ChatCompletionRequest) {
	if invocation == nil {
		return
	}
	invocation.InstanceMode = runtimeAttributionString(req.InstanceMode)
	invocation.RuntimeType = runtimeAttributionString(req.RuntimeType)
	invocation.GatewayID = runtimeAttributionString(req.GatewayID)
	invocation.RuntimePodID = runtimeAttributionInt64(req.RuntimePodID)
	invocation.GatewayKeyID = req.GatewayKeyID
}

func applyAuditRuntimeAttribution(event *models.AuditEvent, req ChatCompletionRequest) {
	if event == nil {
		return
	}
	event.InstanceMode = runtimeAttributionString(req.InstanceMode)
	event.RuntimeType = runtimeAttributionString(req.RuntimeType)
	event.GatewayID = runtimeAttributionString(req.GatewayID)
	event.RuntimePodID = runtimeAttributionInt64(req.RuntimePodID)
	event.GatewayKeyID = req.GatewayKeyID
}

func applyCostRuntimeAttribution(record *models.CostRecord, req ChatCompletionRequest) {
	if record == nil {
		return
	}
	record.InstanceMode = runtimeAttributionString(req.InstanceMode)
	record.RuntimeType = runtimeAttributionString(req.RuntimeType)
	record.GatewayID = runtimeAttributionString(req.GatewayID)
	record.RuntimePodID = runtimeAttributionInt64(req.RuntimePodID)
	record.GatewayKeyID = req.GatewayKeyID
}

func riskHitAttribution(req ChatCompletionRequest) *services.RiskHitAttribution {
	attribution := &services.RiskHitAttribution{
		InstanceMode: runtimeAttributionString(req.InstanceMode),
		RuntimeType:  runtimeAttributionString(req.RuntimeType),
		GatewayID:    runtimeAttributionString(req.GatewayID),
		RuntimePodID: runtimeAttributionInt64(req.RuntimePodID),
	}
	if attribution.InstanceMode == nil && attribution.RuntimeType == nil && attribution.GatewayID == nil && attribution.RuntimePodID == nil {
		return nil
	}
	return attribution
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
		return visionSafePayload(req.RawBody)
	}
	return visionSafePayload([]byte(mustJSONString(req)))
}

func cloneProxyHeaders(headers http.Header) http.Header {
	cloned := make(http.Header)
	copyProxyHeaders(cloned, headers)
	return cloned
}

// setGatewayModelHeaders exposes routing metadata without changing the model
// selected in the client's model picker. This is consumed by runtime UIs to
// show which deployment actually answered an `auto` request.
func setGatewayModelHeaders(headers http.Header, prepared *preparedChatRequest) {
	if prepared == nil || prepared.resolvedModel == nil {
		return
	}
	requested := strings.TrimSpace(prepared.req.Model)
	if requested != "" {
		headers.Set("X-ClawManager-Requested-Model", requested)
	}
	actual := strings.TrimSpace(prepared.resolvedModel.ProviderModelName)
	if actual == "" {
		actual = strings.TrimSpace(prepared.resolvedModel.DisplayName)
	}
	if actual != "" {
		headers.Set("X-ClawManager-Actual-Model", actual)
	}
	if strings.EqualFold(requested, "auto") {
		headers.Set("X-ClawManager-Routing-Mode", "auto")
	} else {
		headers.Set("X-ClawManager-Routing-Mode", "explicit")
	}
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
	_, sessionID := resolveSessionIdentity(req)
	return sessionID
}

func resolveSessionIdentity(req ChatCompletionRequest) (source string, sessionID string) {
	if req.OpenClawSessionKey != nil {
		if key := strings.TrimSpace(*req.OpenClawSessionKey); key != "" {
			return "openclaw_header", utils.NormalizeOpenClawSessionID(key, sessionNormalizationRuntimeType(req))
		}
	}
	if normalized := normalizeOptionalString(req.SessionID); normalized != "" {
		return "explicit", normalizeExistingIdentifier(normalized, "sess")
	}
	if normalized := normalizeOptionalString(req.User); normalized != "" {
		return "openai_user", normalizeExistingIdentifier(normalized, "sess")
	}
	return "", ""
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

// EstimateRequestTokens returns a conservative reservation for Gateway Key TPM
// enforcement. Final usage replaces this reservation when the call completes.
func EstimateRequestTokens(req ChatCompletionRequest) int {
	input, output := routingTokenEstimate(req)
	return input + output
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
	if isImageBlock(part) {
		return "[image omitted]"
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
