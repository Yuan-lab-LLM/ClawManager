package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimePoolOpenCodeNativeStrategyRequiresDedicatedController(t *testing.T) {
	for _, strategy := range []string{"opencode_native_preserve_v1", "legacy_rolling", "unknown"} {
		t.Run(strategy, func(t *testing.T) {
			repo := &runtimePoolHandlerRolloutRepo{}
			handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, repo, nil, &runtimePoolHandlerEvents{})
			router := runtimePoolHandlerRouter(1, "admin", handler)
			body := `{"runtime_type":"opencode","target_image_ref":"registry.example/opencode@sha256:` + strings.Repeat("a", 64) + `","strategy":"` + strategy + `"}`
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/runtime-rollouts", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			expected := http.StatusConflict
			if strategy == "opencode_native_preserve_v1" {
				expected = http.StatusServiceUnavailable
			}
			if response.Code != expected || repo.created != nil {
				t.Fatalf("status=%d unsafe generic rollout=%+v", response.Code, repo.created)
			}
		})
	}
}
