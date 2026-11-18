package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeScaleInStrictAdminBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, submit := range []bool{false, true} {
		for _, tc := range []struct {
			role, body string
			code       int
		}{
			{"user", `{"runtime_type":"hermes","pod_count":1}`, 403},
			{"admin", `{"runtime_type":"hermes","pod_count":1,"slot_count":8}`, 400},
			{"admin", `{"runtime_type":"unknown","pod_count":1}`, 400},
			{"admin", `{"runtime_type":"hermes","pod_count":0}`, 400},
			{"admin", `{"runtime_type":"hermes","pod_count":11}`, 400},
			{"admin", `{"runtime_type":"hermes","pod_count":1.5}`, 400},
			{"admin", `null`, 400},
			{"admin", `{} {}`, 400},
		} {
			router := gin.New()
			h := NewRuntimeManagementHandler(nil)
			router.POST("/", func(c *gin.Context) {
				c.Set("userRole", tc.role)
				if submit {
					h.ScaleInSubmit(c)
				} else {
					h.ScaleInPreflight(c)
				}
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)))
			if w.Code != tc.code {
				t.Fatalf("submit=%v body=%s status=%d", submit, tc.body, w.Code)
			}
		}
	}
}
func TestRuntimeScaleInSubmitRequiresPreviewFingerprint(t *testing.T) {
	router := gin.New()
	h := NewRuntimeManagementHandler(nil)
	router.POST("/", func(c *gin.Context) { c.Set("userRole", "admin"); h.ScaleInSubmit(c) })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"runtime_type":"hermes","pod_count":1}`)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "plan_fingerprint") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
