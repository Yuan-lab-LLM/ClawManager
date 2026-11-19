package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type tracePagingHandlerStub struct {
	stubAIObservabilityHandlerService
}

func (tracePagingHandlerStub) GetTracePage(string, string, int) (*services.AuditTraceDetail, error) {
	return &services.AuditTraceDetail{}, nil
}

func (tracePagingHandlerStub) GetTraceBody(string, string, int, int) (*repository.TraceBodyChunk, error) {
	return &repository.TraceBodyChunk{}, nil
}

func TestTraceFlowRejectsInvalidBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"flow_ids=", "flow_ids=1,0", "flow_ids=1,1", "flow_ids=1,2,3,4,5,6,7,8,9", "flow_ids=1&flow_previous_id=0", "flow_ids=1&flow_previous_id=1"} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai-audit/trace?"+query, nil)
			NewAIObservabilityHandler(&tracePagingHandlerStub{}).GetTraceDetail(c)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
		})
	}
}
