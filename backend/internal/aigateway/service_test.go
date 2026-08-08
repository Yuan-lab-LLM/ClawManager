package aigateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestBuildProviderRequestPreservesToolConfiguration(t *testing.T) {
	req := ChatCompletionRequest{
		Model:   "gateway-model",
		RawBody: []byte(`{"model":"gateway-model","messages":[{"role":"user","content":[{"type":"text","text":"weather in shanghai"}]}],"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"get_weather"}},"stream":true,"stream_options":{"include_usage":false,"custom":"value"},"custom_field":{"nested":[1,2,3]},"session_id":"sess_123","request_id":"req_123","trace_id":"trc_123","instance_id":99,"instance_mode":"lite","runtime_type":"gateway","gateway_id":"gw_123","runtime_pod_id":5}`),
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
	if _, ok := payload["instance_mode"]; ok {
		t.Fatalf("expected internal instance_mode to be stripped")
	}
	if _, ok := payload["runtime_type"]; ok {
		t.Fatalf("expected internal runtime_type to be stripped")
	}
	if _, ok := payload["gateway_id"]; ok {
		t.Fatalf("expected internal gateway_id to be stripped")
	}
	if _, ok := payload["runtime_pod_id"]; ok {
		t.Fatalf("expected internal runtime_pod_id to be stripped")
	}
	if string(payload["stream_options"]) != `{"include_usage":false,"custom":"value"}` {
		t.Fatalf("expected stream_options to pass through unchanged, got %s", string(payload["stream_options"]))
	}
}

func TestBuildProviderRequestEnforcesDeepSeekThinkingSetting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
		want    string
	}{
		{name: "disabled", enabled: false, want: "disabled"},
		{name: "enabled", enabled: true, want: "enabled"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := ChatCompletionRequest{
				Model: "NB",
				RawBody: []byte(`{"model":"NB","messages":[{"role":"user","content":"hello"}],` +
					`"thinking":{"type":"enabled"},"reasoning_effort":"high"}`),
			}
			model := &models.LLMModel{
				ProviderType:      models.ProviderTypeOpenAICompatible,
				ProtocolType:      models.ProtocolTypeOpenAICompatible,
				BaseURL:           "https://api.deepseek.com",
				ProviderModelName: "deepseek-v4-flash",
				ReasoningEnabled:  tc.enabled,
			}
			body, err := buildProviderRequestBody(req, model)
			if err != nil {
				t.Fatalf("buildProviderRequestBody returned error: %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode provider request: %v", err)
			}
			var thinking struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(payload["thinking"], &thinking); err != nil {
				t.Fatalf("decode thinking control: %v", err)
			}
			if thinking.Type != tc.want {
				t.Fatalf("thinking.type = %q, want %q", thinking.Type, tc.want)
			}
			if tc.enabled {
				if string(payload["reasoning_effort"]) != `"high"` {
					t.Fatalf("enabled DeepSeek request must preserve normalized effort, got %s", payload["reasoning_effort"])
				}
			} else if _, exists := payload["reasoning_effort"]; exists {
				t.Fatalf("disabled DeepSeek request retained reasoning_effort")
			}
		})
	}
}

func TestBuildProviderRequestCombinesManagedOpenClawAndModelThinkingSettings(t *testing.T) {
	t.Parallel()
	openclaw := "openclaw"
	tests := []struct {
		name         string
		modelEnabled bool
		raw          string
		wantState    string
		wantEffort   string
	}{
		{name: "model disabled wins", raw: `{"model":"NB","messages":[],"thinking":{"type":"enabled"},"reasoning_effort":"max"}`, wantState: "disabled"},
		{name: "openclaw explicitly off", modelEnabled: true, raw: `{"model":"NB","messages":[],"thinking":{"type":"disabled"},"reasoning_effort":"max"}`, wantState: "disabled"},
		{name: "openclaw omission is off", modelEnabled: true, raw: `{"model":"NB","messages":[]}`, wantState: "disabled"},
		{name: "openclaw high", modelEnabled: true, raw: `{"model":"NB","messages":[],"reasoning_effort":"medium"}`, wantState: "enabled", wantEffort: "high"},
		{name: "openclaw max", modelEnabled: true, raw: `{"model":"NB","messages":[],"reasoning_effort":"xhigh"}`, wantState: "enabled", wantEffort: "max"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := ChatCompletionRequest{Model: "NB", RawBody: []byte(tc.raw), ManagedAgentType: &openclaw}
			model := &models.LLMModel{
				ProviderType: models.ProviderTypeOpenAICompatible, ProtocolType: models.ProtocolTypeOpenAICompatible,
				BaseURL: "https://api.deepseek.com", ProviderModelName: "deepseek-v4-flash", ReasoningEnabled: tc.modelEnabled,
			}
			body, err := buildProviderRequestBody(req, model)
			if err != nil {
				t.Fatalf("buildProviderRequestBody returned error: %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			var thinking struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(payload["thinking"], &thinking); err != nil || thinking.Type != tc.wantState {
				t.Fatalf("thinking = %#v, err=%v, want %q", thinking, err, tc.wantState)
			}
			if tc.wantEffort == "" {
				if _, ok := payload["reasoning_effort"]; ok {
					t.Fatalf("disabled request retained reasoning_effort: %s", payload["reasoning_effort"])
				}
			} else if string(payload["reasoning_effort"]) != `"`+tc.wantEffort+`"` {
				t.Fatalf("reasoning_effort = %s, want %q", payload["reasoning_effort"], tc.wantEffort)
			}
		})
	}
}

func TestBuildProviderRequestNormalizesDeepSeekMaxTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		raw             string
		wantMaxTokens   string
		wantMaxComplete bool
	}{
		{
			name:          "openclaw compatibility field",
			raw:           `{"model":"NB","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":65536}`,
			wantMaxTokens: "65536",
		},
		{
			name:          "explicit provider field wins",
			raw:           `{"model":"NB","messages":[{"role":"user","content":"hello"}],"max_tokens":32768,"max_completion_tokens":65536}`,
			wantMaxTokens: "32768",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, err := buildProviderRequestBody(ChatCompletionRequest{
				Model:   "NB",
				RawBody: []byte(tc.raw),
			}, &models.LLMModel{
				ProviderType:      models.ProviderTypeOpenAICompatible,
				ProtocolType:      models.ProtocolTypeOpenAICompatible,
				BaseURL:           "https://api.deepseek.com",
				ProviderModelName: "deepseek-v4-flash",
			})
			if err != nil {
				t.Fatalf("buildProviderRequestBody returned error: %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode provider request: %v", err)
			}
			if got := string(payload["max_tokens"]); got != tc.wantMaxTokens {
				t.Fatalf("max_tokens = %s, want %s", got, tc.wantMaxTokens)
			}
			if _, exists := payload["max_completion_tokens"]; exists != tc.wantMaxComplete {
				t.Fatalf("max_completion_tokens presence = %v, want %v", exists, tc.wantMaxComplete)
			}
		})
	}
}

func TestBuildProviderRequestPreservesUnknownMaxTokenField(t *testing.T) {
	t.Parallel()
	body, err := buildProviderRequestBody(ChatCompletionRequest{
		Model:   "custom",
		RawBody: []byte(`{"model":"custom","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":24576}`),
	}, &models.LLMModel{
		ProviderType:      models.ProviderTypeOpenAICompatible,
		ProtocolType:      models.ProtocolTypeOpenAICompatible,
		BaseURL:           "https://gateway.example.com/v1",
		ProviderModelName: "custom-model",
	})
	if err != nil {
		t.Fatalf("buildProviderRequestBody returned error: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode provider request: %v", err)
	}
	if got := string(payload["max_completion_tokens"]); got != "24576" {
		t.Fatalf("unknown compatible provider field changed: %s", got)
	}
	if _, exists := payload["max_tokens"]; exists {
		t.Fatal("unknown compatible provider must not inherit DeepSeek token normalization")
	}
}

func TestBuildProviderRequestPreservesUnknownReasoningFields(t *testing.T) {
	t.Parallel()
	req := ChatCompletionRequest{
		Model:   "custom",
		RawBody: []byte(`{"model":"custom","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"medium"}`),
	}
	model := &models.LLMModel{
		ProviderType:      models.ProviderTypeOpenAICompatible,
		ProtocolType:      models.ProtocolTypeOpenAICompatible,
		BaseURL:           "https://gateway.example.com/v1",
		ProviderModelName: "custom-reasoner",
	}
	body, err := buildProviderRequestBody(req, model)
	if err != nil {
		t.Fatalf("buildProviderRequestBody returned error: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode provider request: %v", err)
	}
	if string(payload["reasoning_effort"]) != `"medium"` {
		t.Fatalf("unsupported provider-specific fields must pass through, got %s", payload["reasoning_effort"])
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

func TestRuntimeAttributionHelpersPopulateRecords(t *testing.T) {
	mode := "lite"
	runtimeType := "gateway"
	gatewayID := "gw_instance_1"
	runtimePodID := int64(17)
	req := ChatCompletionRequest{
		InstanceMode: &mode,
		RuntimeType:  &runtimeType,
		GatewayID:    &gatewayID,
		RuntimePodID: &runtimePodID,
	}

	invocation := &models.ModelInvocation{}
	applyInvocationRuntimeAttribution(invocation, req)
	if invocation.InstanceMode == nil || *invocation.InstanceMode != mode {
		t.Fatalf("invocation instance mode not populated")
	}
	if invocation.GatewayID == nil || *invocation.GatewayID != gatewayID {
		t.Fatalf("invocation gateway ID not populated")
	}

	audit := &models.AuditEvent{}
	applyAuditRuntimeAttribution(audit, req)
	if audit.RuntimeType == nil || *audit.RuntimeType != runtimeType {
		t.Fatalf("audit runtime type not populated")
	}
	if audit.RuntimePodID == nil || *audit.RuntimePodID != runtimePodID {
		t.Fatalf("audit runtime pod ID not populated")
	}

	cost := &models.CostRecord{}
	applyCostRuntimeAttribution(cost, req)
	if cost.GatewayID == nil || *cost.GatewayID != gatewayID {
		t.Fatalf("cost gateway ID not populated")
	}

	risk := riskHitAttribution(req)
	if risk == nil || risk.GatewayID == nil || *risk.GatewayID != gatewayID {
		t.Fatalf("risk attribution gateway ID not populated")
	}
}

func TestRewriteStreamLineKeepsToolCalls(t *testing.T) {
	line := "data: {\"id\":\"chatcmpl-1\",\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}}]},\"finish_reason\":null}]}\n"

	var assistantText strings.Builder
	var promptTokens int
	var completionTokens int
	var totalTokens int

	inspection := inspectStreamLine(line, &assistantText, &promptTokens, &completionTokens, &totalTokens)
	if inspection.Done {
		t.Fatalf("expected tool chunk not to finish the stream")
	}
	if inspection.Finished {
		t.Fatalf("expected tool chunk not to carry a terminal finish reason")
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

type flushBuffer struct {
	bytes.Buffer
	flushes int
}

func (b *flushBuffer) Flush() {
	b.flushes++
}

func TestProxyOpenAIStreamAcceptsDoneSentinel(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	output := &flushBuffer{}

	result, err := proxyOpenAIStream(strings.NewReader(input), output, output)
	if err != nil {
		t.Fatalf("proxyOpenAIStream returned error: %v", err)
	}
	if !result.SawDone || !result.Terminal {
		t.Fatalf("expected a verified terminal stream, got %#v", result)
	}
	if result.AssistantText != "hello" {
		t.Fatalf("expected assistant text to be preserved, got %q", result.AssistantText)
	}
	if output.String() != input {
		t.Fatalf("expected provider stream to pass through unchanged")
	}
	if output.flushes == 0 {
		t.Fatalf("expected streaming output to be flushed")
	}
}

func TestProxyOpenAIStreamAcceptsFinishReasonBeforeEOF(t *testing.T) {
	input := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n"
	output := &flushBuffer{}

	result, err := proxyOpenAIStream(strings.NewReader(input), output, output)
	if err != nil {
		t.Fatalf("proxyOpenAIStream returned error: %v", err)
	}
	if result.SawDone || !result.SawFinishReason || !result.Terminal {
		t.Fatalf("expected finish_reason to verify terminal EOF, got %#v", result)
	}
}

func TestProxyOpenAIStreamRejectsTruncatedEOF(t *testing.T) {
	input := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
	output := &flushBuffer{}

	result, err := proxyOpenAIStream(strings.NewReader(input), output, output)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got result=%#v err=%v", result, err)
	}
	if result.Terminal {
		t.Fatalf("truncated stream must not be terminal")
	}
}

func TestProxyOpenAIStreamPreservesProviderErrorFrame(t *testing.T) {
	input := "data: {\"error\":{\"message\":\"provider overloaded\",\"type\":\"server_error\",\"code\":\"overloaded\"}}\n\n"
	output := &flushBuffer{}

	_, err := proxyOpenAIStream(strings.NewReader(input), output, output)
	if err == nil || !strings.Contains(err.Error(), "provider overloaded") {
		t.Fatalf("expected provider stream error, got %v", err)
	}
	if output.String() != input {
		t.Fatalf("expected provider error frame to reach the client unchanged")
	}
}

func TestEmitOpenAIStreamErrorUsesSDKCompatibleEnvelope(t *testing.T) {
	output := &flushBuffer{}
	err := emitOpenAIStreamError(output, output, "trace-123", io.ErrUnexpectedEOF)
	if err != nil {
		t.Fatalf("emitOpenAIStreamError returned error: %v", err)
	}

	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(output.String()), "data:"))
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			TraceID string `json:"trace_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatalf("expected JSON SSE error envelope, got %q: %v", line, err)
	}
	if envelope.Error.Code != upstreamStreamInterruptedCode || envelope.Error.Type != "upstream_stream_error" {
		t.Fatalf("unexpected error envelope: %#v", envelope.Error)
	}
	if envelope.Error.TraceID != "trace-123" {
		t.Fatalf("expected trace id, got %#v", envelope.Error)
	}
	if !strings.Contains(envelope.Error.Message, upstreamStreamInterruptedCode) {
		t.Fatalf("expected stable retry classification in message, got %q", envelope.Error.Message)
	}
}

func TestGatewayUsesSeparateClientsForStreamingAndNonStreaming(t *testing.T) {
	nonStreaming, streaming := newGatewayHTTPClients()
	if nonStreaming.Timeout != 3*time.Minute {
		t.Fatalf("expected bounded non-stream timeout, got %s", nonStreaming.Timeout)
	}
	if streaming.Timeout != 0 {
		t.Fatalf("streaming client must not have a whole-request timeout, got %s", streaming.Timeout)
	}
	if nonStreaming == streaming {
		t.Fatalf("streaming and non-streaming requests must not share one client")
	}
}

type failingWriter struct {
	err error
}

func (w *failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func (w *failingWriter) Flush() {}

func TestProxyOpenAIStreamReturnsDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("client disconnected")
	writer := &failingWriter{err: writeErr}
	_, err := proxyOpenAIStream(
		strings.NewReader("data: [DONE]\n\n"),
		writer,
		writer,
	)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected downstream write error, got %v", err)
	}
}

func TestProxyAnthropicStreamRequiresMessageStop(t *testing.T) {
	input := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":2,"output_tokens":0}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
		"",
	}, "\n")
	output := &flushBuffer{}
	state := &anthropicStreamState{ToolCalls: map[int]*anthropicToolCallState{}}

	_, err := proxyAnthropicStream(strings.NewReader(input), output, output, state)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected incomplete Anthropic stream to fail, got %v", err)
	}
}

func TestProxyAnthropicStreamAcceptsMessageStop(t *testing.T) {
	input := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":2,"output_tokens":0}}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
	output := &flushBuffer{}
	state := &anthropicStreamState{ToolCalls: map[int]*anthropicToolCallState{}}

	_, err := proxyAnthropicStream(strings.NewReader(input), output, output, state)
	if err != nil {
		t.Fatalf("expected message_stop to complete Anthropic stream, got %v", err)
	}
}

var _ http.Flusher = (*flushBuffer)(nil)

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
