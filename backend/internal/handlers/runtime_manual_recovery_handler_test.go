package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManualRuntimeRecoveryStrictAdminAndIdentityBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role, method, path, body string
		actor, code              int
	}{
		{"user", "GET", "/instances/7/runtime-recovery", "", 1, 403},
		{"user", "POST", "/instances/7/runtime-recovery", `{"generation":2}`, 1, 403},
		{"admin", "POST", "/instances/7/runtime-recovery", `null`, 1, 400},
		{"admin", "POST", "/instances/7/runtime-recovery", `{"generation":2,"force":true}`, 1, 400},
		{"admin", "POST", "/instances/7/runtime-recovery", `{"generation":2} {}`, 1, 400},
		{"admin", "POST", "/instances/7/runtime-recovery", `{"generation":2}`, 0, 403},
		{"admin", "POST", "/instances/7/runtime-recovery", `{"generation":2}`, 1, 409},
		{"admin", "GET", "/instances/7/runtime-recovery", "", 1, 409},
		{"admin", "GET", "/instances/no/runtime-recovery", "", 1, 400},
		{"user", "POST", "/runtime-pods/9/recheck", `{"pod_uid":"uid"}`, 1, 403},
		{"admin", "POST", "/runtime-pods/9/recheck", `{"pod_uid":"uid","endpoint":"http://evil"}`, 1, 400},
		{"admin", "POST", "/runtime-pods/9/recheck", `{"pod_uid":"uid"}`, 1, 409},
		{"user", "POST", "/runtime-scheduling/op/retry", `{}`, 1, 403},
		{"admin", "POST", "/runtime-scheduling/op/retry", `{}`, 0, 403},
		{"admin", "POST", "/runtime-scheduling/op/retry", `{}`, 1, 409},
	} {
		t.Run(tc.path+tc.role+tc.body, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set("userRole", tc.role); c.Set("userID", tc.actor) })
			h := &RuntimePoolHandler{}
			m := &RuntimeManagementHandler{}
			r.GET("/instances/:id/runtime-recovery", h.InspectInstanceRecovery)
			r.POST("/instances/:id/runtime-recovery", h.RetryInstanceRecovery)
			r.POST("/runtime-pods/:id/recheck", h.RecheckPod)
			r.POST("/runtime-scheduling/:id/retry", m.Retry)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if w.Code != tc.code {
				t.Fatalf("expected %d got %d %s", tc.code, w.Code, w.Body.String())
			}
		})
	}
}
