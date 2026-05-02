package aigateway

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"clawreef/internal/models"
)

type stubModelInvocationService struct {
	items []models.ModelInvocation
}

func (s *stubModelInvocationService) RecordInvocation(invocation *models.ModelInvocation) error {
	return nil
}

func (s *stubModelInvocationService) GetInvocationByID(id int) (*models.ModelInvocation, error) {
	return nil, nil
}

func (s *stubModelInvocationService) ListInvocationsByTraceID(traceID string) ([]models.ModelInvocation, error) {
	return s.items, nil
}

func (s *stubModelInvocationService) ListInvocationsBySessionID(sessionID string, limit int) ([]models.ModelInvocation, error) {
	return s.items, nil
}

func (s *stubModelInvocationService) ListInvocationsByUserID(userID, limit int) ([]models.ModelInvocation, error) {
	return s.items, nil
}

type stubChatSessionService struct {
	session *models.ChatSession
}

func (s *stubChatSessionService) GetSession(sessionID string) (*models.ChatSession, error) {
	return s.session, nil
}

func (s *stubChatSessionService) EnsureSession(sessionID string, userID, instanceID *int, traceID *string, title *string) (*models.ChatSession, error) {
	return s.session, nil
}

// --- LLMModelRepository stub ---

type stubLLMModelRepository struct {
	activeModels []models.LLMModel
	allModels    []models.LLMModel
	byName       map[string]*models.LLMModel
}

func (r *stubLLMModelRepository) List() ([]models.LLMModel, error) {
	return r.allModels, nil
}

func (r *stubLLMModelRepository) ListActive() ([]models.LLMModel, error) {
	return r.activeModels, nil
}

func (r *stubLLMModelRepository) GetByID(id int) (*models.LLMModel, error) {
	for i := range r.allModels {
		if r.allModels[i].ID == id {
			return &r.allModels[i], nil
		}
	}
	return nil, nil
}

func (r *stubLLMModelRepository) GetByDisplayName(displayName string) (*models.LLMModel, error) {
	if r.byName != nil {
		return r.byName[displayName], nil
	}
	for i := range r.allModels {
		if r.allModels[i].DisplayName == displayName {
			return &r.allModels[i], nil
		}
	}
	return nil, nil
}

func (r *stubLLMModelRepository) Save(model *models.LLMModel) error { return nil }
func (r *stubLLMModelRepository) Delete(id int) error               { return nil }

// --- Tests for ListAvailableModels ---

func TestListAvailableModelsReturnsAutoAndAllActive(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "GPT-4o", ProviderType: "openai", IsActive: true, Priority: 10},
			{ID: 2, DisplayName: "Claude-3", ProviderType: "anthropic", IsActive: true, Priority: 5},
		},
	}
	svc := &service{modelRepo: repo}

	result, err := svc.ListAvailableModels()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 models (Auto + 2 active), got %d", len(result))
	}
	if result[0].DisplayName != "Auto" {
		t.Errorf("first model should be Auto, got %q", result[0].DisplayName)
	}
	if result[1].DisplayName != "GPT-4o" {
		t.Errorf("second model should be GPT-4o, got %q", result[1].DisplayName)
	}
	if result[2].DisplayName != "Claude-3" {
		t.Errorf("third model should be Claude-3, got %q", result[2].DisplayName)
	}
}

func TestListAvailableModelsEmptyWhenNoActive(t *testing.T) {
	repo := &stubLLMModelRepository{activeModels: []models.LLMModel{}}
	svc := &service{modelRepo: repo}

	result, err := svc.ListAvailableModels()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 models when none active, got %d", len(result))
	}
}

// --- Tests for selectAutoModel ---

func TestSelectAutoModelPicksHighestPriority(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "High", Priority: 100, IsSecure: false, IsActive: true},
			{ID: 2, DisplayName: "Low", Priority: 10, IsSecure: false, IsActive: true},
		},
	}
	svc := &service{modelRepo: repo}

	selected, err := svc.selectAutoModel()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.DisplayName != "High" {
		t.Errorf("expected highest priority model 'High', got %q", selected.DisplayName)
	}
}

func TestSelectAutoModelSkipsSecure(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "Secure", Priority: 100, IsSecure: true, IsActive: true},
			{ID: 2, DisplayName: "Normal", Priority: 50, IsSecure: false, IsActive: true},
		},
	}
	svc := &service{modelRepo: repo}

	selected, err := svc.selectAutoModel()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.DisplayName != "Normal" {
		t.Errorf("expected non-secure model 'Normal', got %q", selected.DisplayName)
	}
}

func TestSelectAutoModelFallsBackToSecureWhenNoNonSecure(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "SecureOnly", Priority: 10, IsSecure: true, IsActive: true},
		},
	}
	svc := &service{modelRepo: repo}

	selected, err := svc.selectAutoModel()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.DisplayName != "SecureOnly" {
		t.Errorf("expected fallback to secure model, got %q", selected.DisplayName)
	}
}

func TestSelectAutoModelDistributesAmongSamePriority(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "A", Priority: 100, IsSecure: false, IsActive: true},
			{ID: 2, DisplayName: "B", Priority: 100, IsSecure: false, IsActive: true},
			{ID: 3, DisplayName: "C", Priority: 100, IsSecure: false, IsActive: true},
		},
	}
	svc := &service{modelRepo: repo}

	counts := map[string]int{}
	iterations := 300
	for i := 0; i < iterations; i++ {
		selected, err := svc.selectAutoModel()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[selected.DisplayName]++
	}

	// Each model should be selected at least once in 300 iterations.
	for _, name := range []string{"A", "B", "C"} {
		if counts[name] == 0 {
			t.Errorf("model %q was never selected in %d iterations — load balancing broken", name, iterations)
		}
	}
}

func TestSelectAutoModelExcludingSkipsExcluded(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "Primary", Priority: 100, IsSecure: false, IsActive: true},
			{ID: 2, DisplayName: "Fallback", Priority: 50, IsSecure: false, IsActive: true},
		},
	}
	svc := &service{modelRepo: repo}

	selected, err := svc.selectAutoModelExcluding(map[int]bool{1: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.DisplayName != "Fallback" {
		t.Errorf("expected 'Fallback' after excluding primary, got %q", selected.DisplayName)
	}
}

func TestSelectAutoModelExcludingAllReturnsError(t *testing.T) {
	repo := &stubLLMModelRepository{
		activeModels: []models.LLMModel{
			{ID: 1, DisplayName: "Only", Priority: 100, IsSecure: false, IsActive: true},
		},
	}
	svc := &service{modelRepo: repo}

	_, err := svc.selectAutoModelExcluding(map[int]bool{1: true})
	if err == nil {
		t.Fatal("expected error when all models excluded, got nil")
	}
}

func TestErrProviderConnectionIsDetectable(t *testing.T) {
	var err error = &errProviderConnection{wrapped: errors.New("connection refused")}

	var connErr *errProviderConnection
	if !errors.As(err, &connErr) {
		t.Fatal("errors.As should detect errProviderConnection")
	}
	if connErr.Error() != "connection refused" {
		t.Errorf("unexpected error message: %q", connErr.Error())
	}
}

func TestBuildProviderRequestPreservesToolConfiguration(t *testing.T) {
	req := ChatCompletionRequest{
		Model:   "gateway-model",
		RawBody: []byte(`{"model":"gateway-model","messages":[{"role":"user","content":[{"type":"text","text":"weather in shanghai"}]}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"get_weather"}},"stream":true,"stream_options":{"include_usage":false,"custom":"value"},"custom_field":{"nested":[1,2,3]},"session_id":"sess_123","request_id":"req_123","trace_id":"trc_123","instance_id":99}`),
	}

	model := &models.LLMModel{
		ProviderModelName: "provider-model",
	}

	providerRequestBody, err := buildProviderRequestBody(req, model)
	if err != nil {
		t.Fatalf("buildProviderRequestBody returned error: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(providerRequestBody, &payload); err != nil {
		t.Fatalf("failed to decode provider request body: %v", err)
	}

	var modelName string
	if err := json.Unmarshal(payload["model"], &modelName); err != nil {
		t.Fatalf("failed to decode model name: %v", err)
	}
	if modelName != "provider-model" {
		t.Fatalf("expected provider model name to be replaced, got %q", modelName)
	}
	if _, ok := payload["messages"]; !ok {
		t.Fatalf("expected messages to be forwarded")
	}
	if _, ok := payload["tools"]; !ok {
		t.Fatalf("expected tools to be forwarded")
	}
	if _, ok := payload["tool_choice"]; !ok {
		t.Fatalf("expected tool_choice to be forwarded")
	}
	if _, ok := payload["custom_field"]; !ok {
		t.Fatalf("expected unknown provider fields to survive")
	}
	if _, ok := payload["session_id"]; ok {
		t.Fatalf("expected internal session_id to be stripped")
	}
	if _, ok := payload["request_id"]; ok {
		t.Fatalf("expected internal request_id to be stripped")
	}
	if _, ok := payload["trace_id"]; ok {
		t.Fatalf("expected internal trace_id to be stripped")
	}
	if _, ok := payload["instance_id"]; ok {
		t.Fatalf("expected internal instance_id to be stripped")
	}
	if string(payload["stream_options"]) != `{"include_usage":false,"custom":"value"}` {
		t.Fatalf("expected stream_options to pass through unchanged, got %s", string(payload["stream_options"]))
	}
}

func TestBuildProviderRequestUsesAnthropicProtocolForLocalModel(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "gateway-model",
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "hello from local anthropic",
			},
		},
	}

	model := &models.LLMModel{
		ProviderType:      models.ProviderTypeLocal,
		ProtocolType:      models.ProtocolTypeAnthropic,
		ProviderModelName: "claude-local",
	}

	providerRequestBody, err := buildProviderRequestBody(req, model)
	if err != nil {
		t.Fatalf("buildProviderRequestBody returned error: %v", err)
	}

	var payload anthropicRequestPayload
	if err := json.Unmarshal(providerRequestBody, &payload); err != nil {
		t.Fatalf("expected anthropic payload, got decode error: %v", err)
	}

	if payload.Model != "claude-local" {
		t.Fatalf("expected provider model name to be replaced, got %q", payload.Model)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" {
		t.Fatalf("expected anthropic user message, got %#v", payload.Messages)
	}
}

func TestRewriteStreamLineKeepsToolCalls(t *testing.T) {
	line := "data: {\"id\":\"chatcmpl-1\",\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}}]},\"finish_reason\":null}]}\n"

	var assistantText strings.Builder
	var promptTokens int
	var completionTokens int
	var totalTokens int

	done := inspectStreamLine(line, &assistantText, &promptTokens, &completionTokens, &totalTokens)
	if done {
		t.Fatalf("expected tool chunk not to finish the stream")
	}
	if assistantText.String() != "" {
		t.Fatalf("expected tool chunks not to be appended as assistant text, got %q", assistantText.String())
	}

	payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
	var chunk openAIStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("failed to decode chunk: %v", err)
	}

	if len(chunk.Choices) != 1 || len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected tool_calls to be preserved, got %#v", chunk.Choices)
	}
	if chunk.Choices[0].Delta.ToolCalls[0].Function == nil || chunk.Choices[0].Delta.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("expected function metadata to survive, got %#v", chunk.Choices[0].Delta.ToolCalls[0])
	}
}

func TestExtractAssistantContentFallsBackToToolCalls(t *testing.T) {
	response := ChatCompletionResponse{
		Choices: []struct {
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
					Role:    "assistant",
					Content: nil,
					ToolCalls: []ToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: &ToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Shanghai"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	content := extractAssistantContent(response)
	if !strings.Contains(content, "get_weather") {
		t.Fatalf("expected tool call content to be included, got %q", content)
	}
	if strings.Contains(content, "null") {
		t.Fatalf("expected nil content not to become \"null\", got %q", content)
	}
}

func TestResolveTraceIDReusesRecentTraceForToolLoop(t *testing.T) {
	toolCallID := "call_weather_123"
	svc := &service{
		chatSessionService: &stubChatSessionService{
			session: &models.ChatSession{
				SessionID:   "agent:openclaw:main",
				LastTraceID: stringPtr("trc_existing"),
			},
		},
		invocationService: &stubModelInvocationService{
			items: []models.ModelInvocation{
				{
					TraceID:         "trc_existing",
					InstanceID:      intPtr(9),
					ResponsePayload: stringPtr(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_weather_123","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]}}]}`),
				},
			},
		},
	}

	traceID := svc.resolveTraceID(7, ChatCompletionRequest{
		InstanceID: intPtr(9),
		Messages: []ChatMessage{
			{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    `{"temp_c":25}`,
			},
		},
	}, "agent:openclaw:main")

	if traceID != "trc_existing" {
		t.Fatalf("expected tool continuation to reuse trace trc_existing, got %q", traceID)
	}
}

func TestResolveTraceIDReusesSessionLastTraceBeforeInvocationIsPersisted(t *testing.T) {
	toolCallID := "call_weather_123"
	svc := &service{
		chatSessionService: &stubChatSessionService{
			session: &models.ChatSession{
				SessionID:   "agent:openclaw:main",
				LastTraceID: stringPtr("trc_existing"),
			},
		},
		invocationService: &stubModelInvocationService{},
	}

	traceID := svc.resolveTraceID(7, ChatCompletionRequest{
		InstanceID: intPtr(9),
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "What is the weather in Beijing now?",
			},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID: toolCallID,
						Function: &ToolCallFunction{
							Name:      "get_weather",
							Arguments: `{"city":"Beijing"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    `{"temp_c":25}`,
			},
		},
	}, "agent:openclaw:main")

	if traceID != "trc_existing" {
		t.Fatalf("expected session last trace to be reused before invocation persistence, got %q", traceID)
	}
}

func TestResolveTraceIDReusesTraceWhenOpenClawStripsToolCallUnderscore(t *testing.T) {
	svc := &service{
		invocationService: &stubModelInvocationService{
			items: []models.ModelInvocation{
				{
					TraceID:         "trc_existing",
					UserID:          intPtr(7),
					InstanceID:      intPtr(19),
					ResponsePayload: stringPtr("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_d1cad4719db94d1594f93456\",\"index\":0,\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":\"\"}}]}}]}\n\ndata: [DONE]\n"),
				},
			},
		},
	}

	traceID := svc.resolveTraceID(7, ChatCompletionRequest{
		InstanceID: intPtr(19),
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "What is the weather in Changchun?",
			},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID: "calld1cad4719db94d1594f93456",
						Function: &ToolCallFunction{
							Name:      "exec",
							Arguments: `{"command":"curl -s \"wttr.in/Changchun\""}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "calld1cad4719db94d1594f93456",
				Content:    "changchun: +9C",
			},
		},
	}, "")

	if traceID != "trc_existing" {
		t.Fatalf("expected trace reuse when tool id separators differ, got %q", traceID)
	}
}

func TestResolveTraceIDDoesNotReuseHistoricalToolLoopAcrossNewUserTurn(t *testing.T) {
	toolCallID := "call_weather_123"
	svc := &service{
		chatSessionService: &stubChatSessionService{
			session: &models.ChatSession{
				SessionID:   "agent:openclaw:main",
				LastTraceID: stringPtr("trc_existing"),
			},
		},
		invocationService: &stubModelInvocationService{
			items: []models.ModelInvocation{
				{
					TraceID:         "trc_existing",
					InstanceID:      intPtr(9),
					ResponsePayload: stringPtr(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_weather_123","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]}}]}`),
				},
			},
		},
	}

	traceID := svc.resolveTraceID(7, ChatCompletionRequest{
		InstanceID: intPtr(9),
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "What is the weather in Beijing now?",
			},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID: toolCallID,
						Function: &ToolCallFunction{
							Name:      "get_weather",
							Arguments: `{"city":"Beijing"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: toolCallID,
				Content:    `{"temp_c":25}`,
			},
			{
				Role:    "assistant",
				Content: "Beijing is cloudy and 25C.",
			},
			{
				Role:    "user",
				Content: "How about Shanghai?",
			},
		},
	}, "agent:openclaw:main")

	if traceID == "trc_existing" {
		t.Fatalf("expected a new trace for the next user turn, got %q", traceID)
	}
	if !strings.HasPrefix(traceID, "trc_") {
		t.Fatalf("expected generated trace id to keep trc_ prefix, got %q", traceID)
	}
}

func TestBuildPersistedMessagesKeepsOnlyCurrentTurnMessages(t *testing.T) {
	items := buildPersistedMessages([]ChatMessage{
		{
			Role:    "system",
			Content: "system prompt",
		},
		{
			Role:    "user",
			Content: "Previous turn question",
		},
		{
			Role:    "assistant",
			Content: "Previous turn answer",
		},
		{
			Role:    "user",
			Content: "Current turn question",
		},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID: "call_current",
					Function: &ToolCallFunction{
						Name:      "get_weather",
						Arguments: `{"city":"Shanghai"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			ToolCallID: "call_current",
			Content:    `{"temp_c":16}`,
		},
	})

	if len(items) != 3 {
		t.Fatalf("expected only current turn messages to be persisted, got %d", len(items))
	}
	if items[0].Role != "user" || items[0].Content != "Current turn question" {
		t.Fatalf("expected current turn user message first, got %#v", items[0])
	}
	if items[1].Role != "assistant" || !strings.Contains(items[1].Content, "get_weather") {
		t.Fatalf("expected current turn assistant tool call second, got %#v", items[1])
	}
	if items[2].Role != "tool" || items[2].Content != `{"temp_c":16}` {
		t.Fatalf("expected current turn tool output third, got %#v", items[2])
	}
	if items[0].SequenceNo != 1 || items[1].SequenceNo != 2 || items[2].SequenceNo != 3 {
		t.Fatalf("expected current turn sequence numbers to restart at 1, got %#v", items)
	}
}

func TestResolveTraceIDKeepsExplicitTraceID(t *testing.T) {
	explicit := "trc_explicit"
	svc := &service{}

	traceID := svc.resolveTraceID(7, ChatCompletionRequest{
		TraceID: &explicit,
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}, "")

	if traceID != explicit {
		t.Fatalf("expected explicit trace id %q, got %q", explicit, traceID)
	}
}

func TestResolveSessionIDPrefersExplicitSessionThenOpenAIUser(t *testing.T) {
	explicitSession := "agent:main:direct:alice"
	if got := resolveSessionID(ChatCompletionRequest{SessionID: &explicitSession}); got != explicitSession {
		t.Fatalf("expected explicit session id %q, got %q", explicitSession, got)
	}

	openAIUser := "agent:main:direct:bob"
	if got := resolveSessionID(ChatCompletionRequest{User: &openAIUser}); got != openAIUser {
		t.Fatalf("expected OpenAI user to become session id %q, got %q", openAIUser, got)
	}
}

func TestNormalizeExistingIdentifierHashesLongOpenClawSessionKeys(t *testing.T) {
	longKey := "agent:very-long-openclaw-agent-id:discord:default:direct:12345678901234567890123456789012345678901234567890"
	normalized := normalizeExistingIdentifier(longKey, "sess")
	if normalized == longKey {
		t.Fatalf("expected long session key to be normalized")
	}
	if len(normalized) > maxStoredIdentifierLength {
		t.Fatalf("expected normalized id length <= %d, got %d", maxStoredIdentifierLength, len(normalized))
	}
}
