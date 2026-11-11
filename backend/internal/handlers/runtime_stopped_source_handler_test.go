package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStoppedSourceRecoveryStrictAdminBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role, body string
		code       int
	}{
		{"user", `{}`, 403},
		{"admin", `null`, 400},
		{"admin", `{"successor_generation":8,"source_pod_uid":"old","observer_runtime_pod_id":153,"endpoint":"https://evil"}`, 400},
		{"admin", `{"successor_generation":8,"source_pod_uid":"old","observer_runtime_pod_id":153} {}`, 400},
		{"admin", `{"successor_generation":1,"source_pod_uid":"old","observer_runtime_pod_id":153}`, 400},
		{"admin", `{"successor_generation":8,"source_pod_uid":"old","observer_runtime_pod_id":153}`, 409},
	} {
		router := gin.New()
		h := &RuntimePoolHandler{}
		router.POST("/instances/:id/recover-stopped-source", func(c *gin.Context) { c.Set("userRole", tc.role); c.Set("userID", 1); h.RecoverStoppedSource(c) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/instances/155/recover-stopped-source", strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatalf("role %s body %s: status %d", tc.role, tc.body, w.Code)
		}
	}
}
