package aigateway

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestImageCatalogEligibilityAndRedaction(t *testing.T) {
	for _, name := range []string{"allowed", "inactive", "chat", "no-price", "nan", "negative", "currency", "no-operation", "key-denied", "owner-denied", "checker-failure", "no-checker", "no-owner"} {
		t.Run(name, func(t *testing.T) {
			m := imageRoutingModel()
			price := 0.2
			m.ImagePricePerImage = &price
			m.Currency = "USD"
			m.ImageEditEnabled = true
			secret := "secret-canary"
			m.APIKey = &secret
			m.BaseURL = "https://private-canary.invalid/v1"
			checker := &accessTestChecker{}
			userID := 1
			var groups []string
			switch name {
			case "inactive":
				m.IsActive = false
			case "chat":
				m.ModelKind = models.ModelKindChat
			case "no-price":
				m.ImagePricePerImage = nil
			case "nan":
				price = math.NaN()
			case "negative":
				price = -1
			case "currency":
				m.Currency = "CNY"
			case "no-operation":
				m.ImageGenerationEnabled, m.ImageEditEnabled = false, false
			case "key-denied":
				groups = []string{"not-allowed"}
			case "owner-denied":
				checker.revoked.Store(true)
			case "checker-failure":
				checker.failure = errors.New("database unavailable")
			case "no-owner":
				userID = 0
			}
			s := routingGateway([]models.LLMModel{m}, &stubAuditEventService{})
			s.modelAccessChecker = checker
			if name == "no-checker" {
				s.modelAccessChecker = nil
			}
			result, err := s.ImageCatalog(t.Context(), userID, groups)
			if name == "no-checker" || name == "checker-failure" || name == "no-owner" {
				if err == nil {
					t.Fatal("must fail closed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != "allowed" {
				if len(result) != 0 {
					t.Fatalf("ineligible model exposed: %+v", result)
				}
				return
			}
			if len(result) != 1 || result[0].ID != m.DisplayName || len(result[0].Operations) != 2 || result[0].EstimatedPricePerImage != price {
				t.Fatal(result)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "canary") {
				t.Fatal("provider secrets exposed")
			}
		})
	}
}

func TestImageToolReferencesExcludePixelsAndProviderPrompts(t *testing.T) {
	r := &ImageExecutionResponse{TraceID: "image-test", Data: []imageResultItem{{ArtifactID: "img_test", Base64: "pixels-canary", RevisedPrompt: "prompt-canary"}}}
	raw, err := json.Marshal(r.ToolReferences())
	if err != nil || strings.Contains(string(raw), "canary") || !strings.Contains(string(raw), "img_test") {
		t.Fatal(string(raw), err)
	}
}
