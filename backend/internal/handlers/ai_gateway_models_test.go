package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"clawreef/internal/aigateway"
	"clawreef/internal/models"

	"github.com/gin-gonic/gin"
)

type sdkModelsGateway struct {
	aigateway.Service
	items       []aigateway.AvailableModel
	err         error
	userID      int
	groups      []string
	legacyCalls int
}

func (s *sdkModelsGateway) ListSDKModelsForUser(userID int, groups []string) ([]aigateway.AvailableModel, error) {
	s.userID, s.groups = userID, groups
	return s.items, s.err
}

func (s *sdkModelsGateway) ListAvailableModelsForUser(int, []string) ([]aigateway.AvailableModel, error) {
	s.legacyCalls++
	return []aigateway.AvailableModel{{DisplayName: "legacy-only"}}, nil
}

func TestGatewayModelsOpenAIList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "authorized", true: "empty"}[empty], func(t *testing.T) {
			svc := &sdkModelsGateway{}
			if !empty {
				svc.items = []aigateway.AvailableModel{{ID: 0, DisplayName: "Auto", Provider: "gateway"}, {ID: 17, DisplayName: "deepseek-v4.1-flash", Provider: "openai-compatible"}}
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/gateway/llm/models", nil)
			c.Set("userID", 7)
			c.Set("gatewayAllowedModelGroups", []string{"allowed"})
			(&AIGatewayHandler{service: svc}).ListModels(c)
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			var body struct {
				Object string           `json:"object"`
				Data   []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("not an OpenAI model list: %s: %v", w.Body.String(), err)
			}
			if body.Object != "list" || body.Data == nil || len(body.Data) != len(svc.items) {
				t.Fatalf("invalid model list: %s", w.Body.String())
			}
			if svc.userID != 7 || !reflect.DeepEqual(svc.groups, []string{"allowed"}) || svc.legacyCalls != 0 {
				t.Fatalf("SDK model authorization bypassed: %+v", svc)
			}
			if !empty {
				want := []map[string]any{{"id": "auto", "object": "model", "created": float64(0), "owned_by": "gateway"}, {"id": "deepseek-v4.1-flash", "object": "model", "created": float64(0), "owned_by": "openai-compatible"}}
				if !reflect.DeepEqual(body.Data, want) {
					t.Fatalf("unexpected model identities/metadata: %#v", body.Data)
				}
			}
		})
	}
}

func TestGatewayModelsFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		userID  int
		service aigateway.Service
		want    int
	}{
		{"unauthenticated", 0, &sdkModelsGateway{}, http.StatusUnauthorized},
		{"missing-scoped-catalog", 7, &visionCaptureGateway{}, http.StatusServiceUnavailable},
		{"denied", 7, &sdkModelsGateway{err: models.ErrModelUnavailable}, http.StatusForbidden},
		{"catalog-error", 7, &sdkModelsGateway{err: errors.New("catalog unavailable")}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/gateway/llm/models", nil)
			c.Set("userID", tc.userID)
			(&AIGatewayHandler{service: tc.service}).ListModels(c)
			if w.Code != tc.want {
				t.Fatalf("HTTP %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
