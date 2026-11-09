package handlers

import (
	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"clawreef/internal/utils"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelAccessErrorHasNonRetryableCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	utils.HandleError(c, fmt.Errorf("resolve: %w", models.ErrModelUnavailable))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "model_unavailable") || !strings.Contains(w.Body.String(), "当前模型不可用") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestModelAccessErrorSurvivesProtocolStreamTranslation(t *testing.T) {
	for _, kind := range []string{"responses", "anthropic"} {
		w := httptest.NewRecorder()
		writer := &protocolStreamWriter{ResponseWriter: w, kind: kind}
		_, _ = writer.Write([]byte("event: error\ndata: {\"error\":{\"code\":\"model_unavailable\",\"type\":\"permission_error\",\"message\":\"当前模型不可用\"}}\n\n"))
		if !strings.Contains(w.Body.String(), "model_unavailable") || strings.Contains(w.Body.String(), "response.completed") {
			t.Fatalf("%s: %s", kind, w.Body.String())
		}
	}
}

func TestModelAccessRejectsSpoofedInstanceAttribution(t *testing.T) {
	h := &AIGatewayHandler{}
	for _, bound := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", 1)
		if bound {
			c.Set("instanceID", 8)
		}
		id := 9
		if h.validateGatewayInstanceAttribution(c, &aigateway.ChatCompletionRequest{InstanceID: &id}) || w.Code != http.StatusForbidden {
			t.Fatal("spoofed attribution accepted")
		}
	}
}
