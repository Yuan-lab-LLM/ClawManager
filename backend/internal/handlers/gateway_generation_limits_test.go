package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/aigateway"
	"github.com/gin-gonic/gin"
)

func TestAllProtocolLeasesUseCanonicalOutputLimit(t *testing.T) {
	limit := 12000
	var chat aigateway.ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"auto","messages":[{"role":"user","content":"你好"}],"max_completion_tokens":12000}`), &chat); err != nil {
		t.Fatal(err)
	}
	responses := responsesRequest{Model: "auto", Input: "你好", MaxOutputTokens: &limit}.chatRequest()
	anthropic := anthropicRequest{Model: "auto", MaxTokens: &limit}.chatRequest()
	anthropic.Messages = chat.Messages
	for _, req := range []aigateway.ChatCompletionRequest{chat, responses, anthropic} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Set("gatewayKeyID", "gk-test")
		store := &fakeGatewayRateLimitStore{}
		h := &AIGatewayHandler{rateLimitStore: store}
		if _, ok := h.acquireGatewayKeyLease(c, &req); !ok {
			t.Fatal("valid request rejected")
		}
		if req.MaxTokens == nil || *req.MaxTokens != limit || store.input.EstimatedTokens <= limit || store.input.EstimatedTokens != aigateway.EstimateRequestTokens(req) {
			t.Fatalf("wrong reservation: %+v", store.input)
		}
	}
}

func TestInvalidLimitsRejectedBeforeLeaseEvenWithoutKey(t *testing.T) {
	for _, withKey := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		if withKey {
			c.Set("gatewayKeyID", "gk-test")
		}
		zero := 0
		req := aigateway.ChatCompletionRequest{MaxCompletionTokens: &zero}
		store := &fakeGatewayRateLimitStore{}
		h := &AIGatewayHandler{rateLimitStore: store}
		if _, ok := h.acquireGatewayKeyLease(c, &req); ok || recorder.Code != http.StatusBadRequest || store.input.KeyID != "" {
			t.Fatal("invalid request consumed lease")
		}
	}
}
