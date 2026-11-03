package aigateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

func TestVisionActualProviderRequestAndAudit(t *testing.T) {
	for _, protocol := range []string{models.ProtocolTypeOpenAICompatible, models.ProtocolTypeAnthropic} {
		for _, auto := range []bool{false, true} {
			t.Run(protocol+map[bool]string{true: "/auto", false: "/explicit"}[auto], func(t *testing.T) {
				data := visionTestPNG(t)
				received := make(chan []byte, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					received <- raw
					w.Header().Set("Content-Type", "application/json")
					if protocol == models.ProtocolTypeAnthropic {
						if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-provider-key" {
							t.Errorf("unexpected Anthropic request: %s", r.URL.Path)
						}
						_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"upstream-vision","content":[{"type":"text","text":"two pixels"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":2}}`)
					} else {
						if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-provider-key" {
							t.Errorf("unexpected OpenAI request: %s", r.URL.Path)
						}
						_, _ = io.WriteString(w, `{"id":"chat_1","model":"upstream-vision","choices":[{"index":0,"message":{"role":"assistant","content":"two pixels"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`)
					}
				}))
				defer upstream.Close()
				yes := true
				key := "test-provider-key"
				model := models.LLMModel{ID: 1, DisplayName: "vision", ModelGroup: "vision", ProviderType: models.ProviderTypeLocal, ProtocolType: protocol, ProviderModelName: "upstream-vision", BaseURL: upstream.URL + "/v1", APIKey: &key, VisionSupportOverride: &yes, IsActive: true, Currency: "USD", QualityTier: 2}
				audit := &stubAuditEventService{}
				s := routingGateway([]models.LLMModel{model}, audit)
				costs := &routingCostService{}
				s.costRecordService = costs
				s.httpClient = upstream.Client()
				s.deploymentStore = services.NewNoopGatewayDeploymentStore()
				req := ChatCompletionRequest{Model: "vision", Messages: []ChatMessage{{Role: "user", Content: []interface{}{map[string]interface{}{"type": "text", "text": "describe"}, visionTestBlock("data:image/png;base64," + data)}}}}
				if auto {
					req.Model = "auto"
				}
				response, _, err := s.ChatCompletions(t.Context(), 1, req)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 200 {
					t.Fatal(string(response.Body))
				}
				raw := <-received
				if !strings.Contains(string(raw), data) || !strings.Contains(string(raw), "upstream-vision") {
					t.Fatal("upstream image or physical model lost")
				}
				if protocol == models.ProtocolTypeAnthropic && (!strings.Contains(string(raw), `"type":"base64"`) || strings.Contains(string(raw), `"image_url"`)) {
					t.Fatal("not translated to Anthropic image source")
				}
				stored, _ := json.Marshal(s.invocationService.(*stubModelInvocationService).items)
				events, _ := json.Marshal(audit.events)
				if strings.Contains(string(stored), data) || strings.Contains(string(events), data) {
					t.Fatal("image leaked into persisted audit")
				}
				if len(costs.records) != 1 {
					t.Fatal("missing usage cost record")
				}
			})
		}
	}
}
