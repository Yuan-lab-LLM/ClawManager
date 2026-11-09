package aigateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
)

const modelPermissionError = `{"error":{"code":"permission_error","message":"This token does not have permission to use the model: primary","type":"permission_error"}}`

type permissionInvocationRecorder struct{ stubModelInvocationService }

func (s *permissionInvocationRecorder) RecordInvocation(item *models.ModelInvocation) error {
	s.items = append(s.items, *item)
	return nil
}

func TestProviderPermissionClassification(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{modelPermissionError, true},
		{`{"error":{"type":"permission_error","code":403}}`, true},
		{`{"error":{"code":"model_access_denied"}}`, true},
		{`{"error":{"type":"content_policy_violation"}}`, false},
		{`{"error":{"message":"permission_error"}}`, false},
		{"Forbidden", false}, {"<html>WAF block</html>", false}, {"{}", false},
	} {
		response := &ProxyResponse{StatusCode: 403, Body: []byte(tc.body)}
		if isRetryableProviderResponse(response) != tc.want || shouldPenalizeDeployment(t.Context(), 403, nil, response) != tc.want {
			t.Fatalf("wrong classification for %s", tc.body)
		}
	}
	response := &ProxyResponse{StatusCode: 403, Body: []byte(modelPermissionError)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if shouldPenalizeDeployment(ctx, 403, nil, response) {
		t.Fatal("cancelled request penalized")
	}
	if shouldPenalizeDeployment(t.Context(), 403, &downstreamStreamWriteError{err: io.ErrClosedPipe}, response) {
		t.Fatal("client disconnection penalized")
	}
	if isRetryableProviderResponse(&ProxyResponse{StatusCode: 400, Body: response.Body}) {
		t.Fatal("400 must remain terminal")
	}
}

func TestProviderPermissionFailoverHTTP(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, protocol := range []string{models.ProviderTypeOpenAICompatible, models.ProviderTypeAnthropic} {
			for _, scenario := range []string{"fallback", "all_denied", "no_authorized_fallback", "local_deny", "unknown_403", "policy_403"} {
				t.Run(fmt.Sprintf("stream=%v/%s/%s", stream, protocol, scenario), func(t *testing.T) {
					calls := []string{}
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls = append(calls, r.URL.Path)
						if len(calls) == 1 || scenario == "all_denied" {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(403)
							body := modelPermissionError
							if scenario == "unknown_403" {
								body = "Forbidden"
							}
							if scenario == "policy_403" {
								body = `{"error":{"type":"content_policy_violation"}}`
							}
							_, _ = io.WriteString(w, body)
							return
						}
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"fallback success\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						} else {
							_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"fallback success"}}]}`)
						}
					}))
					defer upstream.Close()
					items := routingModels()
					items[0].ProviderType = protocol
					for i := range items {
						items[i].BaseURL = upstream.URL
						items[i].ProviderModelName = items[i].DisplayName
					}
					audit := &stubAuditEventService{}
					gateway := routingGateway(items, audit)
					invocations := &permissionInvocationRecorder{}
					gateway.invocationService = invocations
					store := &routingDeploymentStore{}
					gateway.deploymentStore, gateway.costRecordService = store, &routingCostService{}
					gateway.httpClient, gateway.streamHTTPClient = upstream.Client(), upstream.Client()
					req := routingRequest("hello")
					req.Stream = stream
					if scenario == "no_authorized_fallback" {
						req.AllowedModelGroups = []string{"basic"}
					}
					if scenario == "local_deny" {
						req.AllowedModelGroups = []string{"not-allowed"}
					}
					status, body := 0, ""
					var err error
					if stream {
						out := httptest.NewRecorder()
						_, err = gateway.StreamChatCompletions(t.Context(), 1, req, out)
						status, body = out.Code, out.Body.String()
					} else {
						var response *ProxyResponse
						response, _, err = gateway.ChatCompletions(t.Context(), 1, req)
						if response != nil {
							status, body = response.StatusCode, string(response.Body)
						}
					}
					if scenario == "local_deny" {
						if err == nil || len(calls) != 0 || len(store.observed) != 0 {
							t.Fatal("local authorization bypassed")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					wantCalls := 1
					if scenario == "fallback" || scenario == "all_denied" {
						wantCalls = 2
					}
					if len(calls) != wantCalls {
						t.Fatalf("calls=%v expected %d", calls, wantCalls)
					}
					if scenario == "fallback" {
						if status != 200 || !strings.Contains(body, "fallback success") || strings.Contains(body, "permission_error") {
							t.Fatalf("bad fallback response: %d %s", status, body)
						}
						if len(invocations.items) != 2 || invocations.items[1].Status != models.ModelInvocationStatusCompleted || invocations.items[1].ActualProviderModel != items[1].ProviderModelName {
							t.Fatalf("actual model not recorded: %+v", invocations.items)
						}
					} else if status != 403 {
						t.Fatalf("original error lost: %d %s", status, body)
					}
					if len(store.observed) != wantCalls {
						t.Fatal("missing deployment observations")
					}
					first := store.observed[0]
					wantCooldown := scenario != "unknown_403" && scenario != "policy_403"
					if first.ImmediateCooldown != wantCooldown || first.PenalizeFailure != wantCooldown || first.LatencyMs != 0 {
						t.Fatalf("incorrect permission health: %+v", first)
					}
					failovers := 0
					for _, event := range audit.events {
						if event.EventType == "gateway.deployment.failover" {
							failovers++
						}
					}
					if failovers != wantCalls-1 {
						t.Fatalf("failover events=%d", failovers)
					}
				})
			}
		}
	}
}
