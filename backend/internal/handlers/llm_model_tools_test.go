package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type toolOverrideCaptureService struct {
	services.LLMModelService
	requests []services.SaveLLMModelRequest
}

func (s *toolOverrideCaptureService) SaveModel(req services.SaveLLMModelRequest) (*models.LLMModel, error) {
	s.requests = append(s.requests, req)
	return &models.LLMModel{ID: len(s.requests), ToolsSupportOverride: req.ToolsSupportOverride}, nil
}

func TestModelHandlersForwardToolOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, batch := range []bool{false, true} {
		for _, value := range []string{"true", "false", "null"} {
			service := &toolOverrideCaptureService{}
			handler := NewLLMModelHandler(service)
			payload := `{"display_name":"model","provider_type":"local","base_url":"http://localhost","provider_model_name":"model","tools_support_override":` + value + `}`
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
			if recorder.Code != http.StatusOK || len(service.requests) != 1 {
				t.Fatalf("batch=%v value=%s response=%d %s", batch, value, recorder.Code, recorder.Body.String())
			}
			got := service.requests[0].ToolsSupportOverride
			if value == "null" {
				if got != nil {
					t.Fatal("null override must remain unset")
				}
			} else if got == nil || *got != (value == "true") {
				t.Fatalf("batch=%v lost override=%s", batch, value)
			}
		}
	}
}
