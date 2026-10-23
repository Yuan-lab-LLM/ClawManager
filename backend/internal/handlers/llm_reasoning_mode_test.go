package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestModelHandlersForwardReasoningMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, batch := range []bool{false, true} {
		for _, mode := range []string{"", "none", "enable-thinking"} {
			svc := &toolOverrideCaptureService{}
			handler := NewLLMModelHandler(svc)
			payload := `{"display_name":"model","provider_type":"local","base_url":"https://proxy.example","provider_model_name":"model","reasoning_enabled":false,"reasoning_control_mode":"` + mode + `"}`
			if batch {
				payload = `{"models":[` + payload + `]}`
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/models", strings.NewReader(payload))
			ctx.Request.Header.Set("Content-Type", "application/json")
			if batch {
				handler.BatchUpsertModels(ctx)
			} else {
				handler.UpsertModel(ctx)
			}
			if recorder.Code != http.StatusOK || len(svc.requests) != 1 {
				t.Fatalf("bad response: %s", recorder.Body.String())
			}
			req := svc.requests[0]
			if req.ReasoningControlMode == nil || *req.ReasoningControlMode != mode || req.ReasoningEnabled == nil || *req.ReasoningEnabled {
				t.Fatal("lost mode or explicit false")
			}
		}
	}
}

func TestGatewayProtocolsRetainReasoningOptOut(t *testing.T) {
	var messages anthropicRequest
	if err := json.Unmarshal([]byte(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"}}`), &messages); err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	_ = json.Unmarshal(messages.chatRequest().RawBody, &body)
	if string(body["thinking"]) != `{"type":"disabled"}` {
		t.Fatal("Messages lost thinking opt-out")
	}
	var responses responsesRequest
	if err := json.Unmarshal([]byte(`{"model":"auto","input":"hi","reasoning":{"effort":"none"}}`), &responses); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(responses.chatRequest().RawBody, &body)
	if string(body["reasoning_effort"]) != `"none"` {
		t.Fatal("Responses lost reasoning opt-out")
	}
}
