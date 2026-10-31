package handlers

import (
	"clawreef/internal/aigateway"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type visionCaptureGateway struct {
	request aigateway.ChatCompletionRequest
	owner   int
}

func (s *visionCaptureGateway) ListAvailableModels() ([]aigateway.AvailableModel, error) {
	return nil, nil
}
func (s *visionCaptureGateway) ChatCompletions(_ context.Context, owner int, req aigateway.ChatCompletionRequest) (*aigateway.ProxyResponse, string, error) {
	s.request = req
	s.owner = owner
	return &aigateway.ProxyResponse{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"id":"test","model":"vision","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`)}, "trace", nil
}
func (s *visionCaptureGateway) StreamChatCompletions(context.Context, int, aigateway.ChatCompletionRequest, http.ResponseWriter) (string, error) {
	panic("unexpected stream")
}

func TestVisionThreeProtocolEntrypointsPreserveImagesAndOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range []string{"chat", "responses", "anthropic"} {
		t.Run(entry, func(t *testing.T) {
			svc := &visionCaptureGateway{}
			h := &AIGatewayHandler{service: svc}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set("userID", 7)
			content := `[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://example.com/fixture.png"}}]`
			body := `{"model":"auto","messages":[{"role":"user","content":` + content + `}]}`
			if entry == "responses" {
				body = `{"model":"auto","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":"https://example.com/fixture.png"}]}]}`
			}
			if entry == "anthropic" {
				body = `{"model":"auto","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image","source":{"type":"url","url":"https://example.com/fixture.png"}}]}]}`
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/"+entry, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			switch entry {
			case "chat":
				h.ChatCompletions(c)
			case "responses":
				h.Responses(c)
			case "anthropic":
				h.AnthropicMessages(c)
			}
			if rec.Code != 200 {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if svc.owner != 7 || len(svc.request.Messages) != 1 || !aigateway.HasImageContent(svc.request.Messages[0].Content) {
				t.Fatal("image lost at protocol edge")
			}
			raw, _ := json.Marshal(svc.request.Messages)
			if !strings.Contains(string(raw), "https://example.com/fixture.png") || !strings.Contains(string(raw), "describe") {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestAnthropicVisionAndToolImagesAreNotFlattened(t *testing.T) {
	var payload anthropicRequest
	raw := `{"model":"vision","messages":[{"role":"user","content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}},{"type":"text","text":"after"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":[{"type":"image","source":{"type":"url","url":"https://example.com/tool.png"}}]}]}]}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	req := payload.chatRequest()
	if err := aigateway.NormalizeVisionRequest(&req); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages %v", req.Messages)
	}
	blocks := req.Messages[0].Content.([]interface{})
	if len(blocks) != 3 || blocks[1].(map[string]interface{})["type"] != "image_url" {
		t.Fatal("image or ordering lost")
	}
	if req.Messages[1].Role != "tool" || req.Messages[1].Content.([]interface{})[0].(map[string]interface{})["type"] != "image_url" {
		t.Fatal("tool result image lost")
	}
}

func TestModelHandlersForwardVisionOverrideAndReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, batch := range []bool{false, true} {
		for _, value := range []string{"true", "false", "null"} {
			svc := &toolOverrideCaptureService{}
			handler := NewLLMModelHandler(svc)
			body := `{"display_name":"a","provider_type":"local","base_url":"http://localhost","provider_model_name":"a","vision_support_override":` + value + `,"reset_vision_support_override":true}`
			if batch {
				body = `{"models":[` + body + `]}`
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/models", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			if batch {
				handler.BatchUpsertModels(c)
			} else {
				handler.UpsertModel(c)
			}
			if recorder.Code != 200 || len(svc.requests) != 1 {
				t.Fatalf("bad response %s", recorder.Body.String())
			}
			got := svc.requests[0]
			if !got.ResetVisionSupportOverride || (value == "null" && got.VisionSupportOverride != nil) || (value != "null" && (got.VisionSupportOverride == nil || *got.VisionSupportOverride != (value == "true"))) {
				t.Fatal("lost vision choice")
			}
		}
	}
}

func TestResponsesRejectsUnsupportedImageFilesBeforeCallingProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"auto","input":[{"role":"user","content":[{"type":"input_image","file_id":"file-foreign"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	(&AIGatewayHandler{}).Responses(c)
	if r.Code != 400 || !strings.Contains(r.Body.String(), "invalid_image_input") {
		t.Fatalf("response %d %s", r.Code, r.Body.String())
	}
}

func TestResponsesRejectsUnsupportedNativeImageGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"auto","input":"draw a cat","tools":[{"type":"image_generation"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	(&AIGatewayHandler{}).Responses(c)
	if r.Code != 400 || !strings.Contains(r.Body.String(), "unsupported_image_generation_tool") {
		t.Fatalf("response %d %s", r.Code, r.Body.String())
	}
}
