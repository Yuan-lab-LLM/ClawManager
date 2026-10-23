package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/aigateway"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type failingProtocolResponseWriter struct {
	header http.Header
	err    error
}

func (w *failingProtocolResponseWriter) Header() http.Header       { return w.header }
func (*failingProtocolResponseWriter) WriteHeader(int)             {}
func (w *failingProtocolResponseWriter) Write([]byte) (int, error) { return 0, w.err }

type fakeGatewayRateLimitStore struct {
	input           services.GatewayRateLimitInput
	finalizedTokens int
	releaseCount    int
}

func (s *fakeGatewayRateLimitStore) Acquire(_ context.Context, input services.GatewayRateLimitInput) (*services.GatewayRateLimitLease, error) {
	s.input = input
	return &services.GatewayRateLimitLease{}, nil
}
func (s *fakeGatewayRateLimitStore) Finalize(_ context.Context, _ *services.GatewayRateLimitLease, actualTokens int) error {
	s.finalizedTokens = actualTokens
	return nil
}
func (s *fakeGatewayRateLimitStore) Release(_ context.Context, _ *services.GatewayRateLimitLease) error {
	s.releaseCount++
	return nil
}

type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
}

func newResponseRecorder() *responseRecorder            { return &responseRecorder{header: make(http.Header)} }
func (r *responseRecorder) Header() http.Header         { return r.header }
func (r *responseRecorder) Write(p []byte) (int, error) { return r.body.Write(p) }
func (r *responseRecorder) WriteHeader(int)             {}
func contains(s, part string) bool                      { return strings.Contains(s, part) }

func TestGatewayUserIDRequiresPositiveAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := gatewayUserID(c); ok {
		t.Fatal("missing user identity must not be accepted")
	}
	c.Set("userID", 0)
	if _, ok := gatewayUserID(c); ok {
		t.Fatal("zero user identity must not be accepted")
	}
	c.Set("userID", 42)
	if id, ok := gatewayUserID(c); !ok || id != 42 {
		t.Fatalf("gatewayUserID = %d/%v, want 42/true", id, ok)
	}
}

func TestGatewayKeyLeaseCarriesTPMAndFinalizesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/gateway/llm/chat/completions", nil)
	c.Set("gatewayKeyID", "gk_test")
	c.Set("gatewayKeyRPMLimit", 10)
	c.Set("gatewayKeyTPMLimit", 2000)
	c.Set("gatewayKeyConcurrencyLimit", 2)
	store := &fakeGatewayRateLimitStore{}
	handler := &AIGatewayHandler{rateLimitStore: store}
	maxTokens := 100
	req := aigateway.ChatCompletionRequest{RawBody: []byte(`{"model":"general","messages":[{"role":"user","content":"hello"}]}`), MaxTokens: &maxTokens}
	lease, ok := handler.acquireGatewayKeyLease(c, &req)
	if !ok || lease == nil {
		t.Fatal("expected Gateway Key lease")
	}
	if store.input.TPMLimit != 2000 || store.input.EstimatedTokens <= maxTokens {
		t.Fatalf("unexpected rate-limit input: %#v", store.input)
	}
	handler.attachGatewayUsageFinalizer(&req, lease)
	if req.UsageFinalizer == nil {
		t.Fatal("usage finalizer was not attached")
	}
	if err := req.UsageFinalizer(42); err != nil {
		t.Fatalf("usage finalizer returned error: %v", err)
	}
	handler.releaseGatewayKeyLease(lease)
	if store.finalizedTokens != 42 || store.releaseCount != 1 {
		t.Fatalf("finalized=%d released=%d", store.finalizedTokens, store.releaseCount)
	}
}

func TestResponsesRequestNormalizesToolRounds(t *testing.T) {
	p := responsesRequest{
		Model: "gateway-model",
		Input: []interface{}{
			map[string]interface{}{"role": "user", "type": "message", "content": "weather"},
			map[string]interface{}{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": `{"city":"Shanghai"}`},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "sunny"},
		},
		Tools: json.RawMessage(`[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]`),
	}
	req := p.chatRequest()
	if len(req.Messages) != 3 || req.Messages[1].ToolCalls[0].Function.Name != "get_weather" || req.Messages[2].ToolCallID != "call_1" {
		t.Fatalf("tool round was not normalized: %#v", req.Messages)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(req.RawBody, &raw); err != nil || raw["messages"] == nil || raw["tools"] == nil {
		t.Fatalf("canonical provider body missing fields: %v %#v", err, raw)
	}
}

func TestAnthropicRequestNormalizesToolUseAndResult(t *testing.T) {
	p := anthropicRequest{Model: "gateway-model", Messages: []struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"`
	}{
		{Role: "assistant", Content: []interface{}{map[string]interface{}{"type": "tool_use", "id": "tool_1", "name": "lookup", "input": map[string]interface{}{"q": "x"}}}},
		{Role: "user", Content: []interface{}{map[string]interface{}{"type": "tool_result", "tool_use_id": "tool_1", "content": "result"}}},
	}}
	req := p.chatRequest()
	if len(req.Messages) != 2 || req.Messages[0].ToolCalls[0].ID != "tool_1" || req.Messages[1].Role != "tool" || req.Messages[1].ToolCallID != "tool_1" {
		t.Fatalf("Anthropic tool blocks were not normalized: %#v", req.Messages)
	}
}

func TestResponsesRequestExtractsInputTextBlocks(t *testing.T) {
	req := (responsesRequest{Model: "gateway-model", Input: []interface{}{map[string]interface{}{
		"type": "message", "role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "hello"}},
	}}}).chatRequest()
	blocks, ok := req.Messages[0].Content.([]interface{})
	if !ok || len(blocks) != 1 || blocks[0].(map[string]interface{})["text"] != "hello" {
		t.Fatalf("content = %#v, want normalized text block", req.Messages[0].Content)
	}
}

func TestResponsesRequestNormalizesDeveloperRoleToSystem(t *testing.T) {
	messages := responseInputMessages([]interface{}{map[string]interface{}{
		"type": "message", "role": "developer", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "managed instructions"}},
	}})
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Fatalf("expected developer role to be normalized to system, got %q", messages[0].Role)
	}
	content, ok := messages[0].Content.([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected normalized content: %#v", messages[0].Content)
	}
	block, ok := content[0].(map[string]interface{})
	if !ok || block["text"] != "managed instructions" {
		t.Fatalf("unexpected normalized content block: %#v", content[0])
	}
}

func TestResponsesRequestPreservesImageInputBlock(t *testing.T) {
	req := (responsesRequest{Model: "gateway-model", Input: []interface{}{map[string]interface{}{
		"type": "message", "role": "user", "content": []interface{}{map[string]interface{}{"type": "input_image", "image_url": "https://example.test/image.png", "detail": "low"}},
	}}}).chatRequest()
	blocks, ok := req.Messages[0].Content.([]interface{})
	if !ok || len(blocks) != 1 {
		t.Fatalf("content = %#v", req.Messages[0].Content)
	}
	image := blocks[0].(map[string]interface{})["image_url"].(map[string]interface{})
	if image["url"] != "https://example.test/image.png" {
		t.Fatalf("image = %#v", image)
	}
}

func TestResponsesRequestMapsReasoningAndStructuredOutput(t *testing.T) {
	p := responsesRequest{Model: "gateway-model", Input: "hello", ParallelToolCalls: boolPointer(true)}
	p.Reasoning.Effort = "high"
	p.Text.Format = json.RawMessage(`{"type":"json_schema","name":"answer","schema":{"type":"object"}}`)
	req := p.chatRequest()
	if req.ReasoningEffort == nil || *req.ReasoningEffort != "high" || req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("request controls = %#v", req)
	}
	if !strings.Contains(string(req.ResponseFormat), "json_schema") {
		t.Fatalf("response format = %s", req.ResponseFormat)
	}
}

func TestAnthropicRequestKeepsAllToolResults(t *testing.T) {
	req := (anthropicRequest{Model: "gateway-model", Messages: []struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"`
	}{{Role: "user", Content: []interface{}{
		map[string]interface{}{"type": "tool_result", "tool_use_id": "one", "content": "first"}, map[string]interface{}{"type": "tool_result", "tool_use_id": "two", "content": "second"},
	}}}}).chatRequest()
	if len(req.Messages) != 2 || req.Messages[0].ToolCallID != "one" || req.Messages[1].ToolCallID != "two" {
		t.Fatalf("tool results = %#v", req.Messages)
	}
}

func TestProtocolStreamWriterDoesNotRepeatAnthropicToolStart(t *testing.T) {
	recorder := newResponseRecorder()
	w := &protocolStreamWriter{ResponseWriter: recorder, kind: "anthropic", model: "gateway-model"}
	_, _ = w.Write([]byte("data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\"}}]}}]}\n\ndata: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
	if got := strings.Count(recorder.body.String(), "event: content_block_start"); got != 1 {
		t.Fatalf("tool start count = %d", got)
	}
	if !contains(recorder.body.String(), "content_block_stop") {
		t.Fatalf("missing content block stop")
	}
}

func TestProtocolStreamWriterUsesDistinctResponsesToolIndex(t *testing.T) {
	recorder := newResponseRecorder()
	w := &protocolStreamWriter{ResponseWriter: recorder, kind: "responses", model: "gateway-model"}
	_, _ = w.Write([]byte("data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"content\":\"x\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
	if !contains(recorder.body.String(), `"output_index":1`) || !contains(recorder.body.String(), "response.function_call_arguments.done") {
		t.Fatalf("missing distinct tool index/events: %s", recorder.body.String())
	}
}

func boolPointer(value bool) *bool { return &value }

func TestProtocolErrorsUseClientProtocolShapes(t *testing.T) {
	if got := responsesError([]byte(`{"error":{"message":"rate limited"}}`)); got["type"] != "error" {
		t.Fatalf("responses error = %#v", got)
	}
	if got := anthropicError([]byte(`{"message":"bad request"}`)); got["type"] != "error" {
		t.Fatalf("anthropic error = %#v", got)
	}
}

func TestWorkspaceInputFilePathAcceptsOnlyCurrentInstanceReferences(t *testing.T) {
	for _, reference := range []string{
		"workspace://notes/readme.md",
		"workspace:notes/readme.md",
		"/api/v1/instances/42/workspace/download?path=notes%2Freadme.md",
	} {
		path, err := workspaceInputFilePath(reference, 42)
		if err != nil || path != "notes/readme.md" {
			t.Fatalf("reference %q = %q, %v", reference, path, err)
		}
	}
	for _, reference := range []string{
		"https://example.test/file.txt", "workspace://../secret.txt", "/api/v1/instances/41/workspace/download?path=notes/readme.md",
	} {
		if _, err := workspaceInputFilePath(reference, 42); err == nil {
			t.Fatalf("expected %q to be rejected", reference)
		}
	}
}

func TestProtocolStreamWriterEmitsResponsesEvents(t *testing.T) {
	recorder := newResponseRecorder()
	w := &protocolStreamWriter{ResponseWriter: recorder, kind: "responses", model: "gateway-model"}
	_, _ = w.Write([]byte("data: {\"id\":\"chat_1\",\"model\":\"gateway-model\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	got := recorder.body.String()
	for _, event := range []string{"response.created", "response.output_text.delta", "response.completed"} {
		if !contains(got, event) {
			t.Fatalf("missing %s in %s", event, got)
		}
	}
}

func TestProtocolStreamWriterPropagatesDownstreamWriteFailure(t *testing.T) {
	wantErr := errors.New("client disconnected")
	w := &protocolStreamWriter{
		ResponseWriter: &failingProtocolResponseWriter{header: make(http.Header), err: wantErr},
		kind:           "responses",
		model:          "gateway-model",
	}
	_, err := w.Write([]byte("data: {\"id\":\"chat_1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("stream write error = %v, want %v", err, wantErr)
	}
	if _, err = w.Write([]byte("data: [DONE]\n\n")); !errors.Is(err, wantErr) {
		t.Fatalf("subsequent stream write error = %v, want %v", err, wantErr)
	}
}
