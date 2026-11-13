package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeRolloutPrepareRejectsInvalidAndUnavailableRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		body   string
		status int
	}{{`{`, 400}, {`{}`, 400}, {`{"runtime_type":"untrusted"}`, 409}, {`{"runtime_type":"opencode"}`, 409}, {`{"runtime_type":"hermes"}`, 409}} {
		r := gin.New()
		r.POST("/prepare", NewRuntimeManagementHandler(nil).PrepareRollout)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/prepare", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
}

func TestRuntimeDSHRolloutRejectsMutableTargetBeforeAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{"registry/dsh:latest", "registry/dsh@sha256:invalid"} {
		store := &runtimePoolHandlerRolloutRepo{}
		handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, store, nil, &runtimePoolHandlerEvents{})
		router := runtimePoolHandlerRouter(7, "admin", handler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/runtime-rollouts", strings.NewReader(`{"runtime_type":"deepseek-harness","target_image_ref":"`+target+`"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusConflict || store.created != nil {
			t.Fatalf("mutable target admitted: %d %s", w.Code, w.Body.String())
		}
	}
}
