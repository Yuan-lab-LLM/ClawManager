package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestModelHandlersForwardImageOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, batch := range []bool{true, false} {
		for _, fields := range []string{``, `,"model_kind":"image","image_generation_enabled":true,"image_edit_enabled":false,"image_price_per_image":0`} {
			svc := &toolOverrideCaptureService{}
			h := NewLLMModelHandler(svc)
			body := `{"display_name":"image","provider_type":"local","base_url":"https://example.com/v1","provider_model_name":"native"` + fields + `}`
			if batch {
				body = `{"models":[` + body + `]}`
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/models", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			if batch {
				h.BatchUpsertModels(c)
			} else {
				h.UpsertModel(c)
			}
			if w.Code != http.StatusOK || len(svc.requests) != 1 {
				t.Fatal(w.Code, w.Body.String())
			}
			got := svc.requests[0]
			if fields == "" {
				if got.ModelKind != nil || got.ImageGenerationEnabled != nil || got.ImageEditEnabled != nil {
					t.Fatal("omitted fields must preserve existing values", got)
				}
			} else if got.ModelKind == nil || *got.ModelKind != "image" || got.ImageGenerationEnabled == nil || !*got.ImageGenerationEnabled || got.ImageEditEnabled == nil || *got.ImageEditEnabled {
				t.Fatal("lost image operations", got)
			}
			if fields != "" && (got.ImagePricePerImage == nil || *got.ImagePricePerImage != 0) {
				t.Fatal("explicit free image price lost", got)
			}
		}
	}
}
