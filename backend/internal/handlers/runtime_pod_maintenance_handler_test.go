package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimePodMaintenanceRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{"pools", "options", "create", "delete", "pool"} {
		h := NewRuntimePodMaintenanceHandler(nil)
		router := gin.New()
		router.Any("/", func(c *gin.Context) {
			c.Set("userRole", "user")
			switch method {
			case "pools":
				h.ManagedPools(c)
			case "options":
				h.ScaleOptions(c)
			case "create":
				h.Scale(c)
			case "delete":
				h.Delete(c)
			case "pool":
				h.DeletePool(c)
			}
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
		if w.Code != 403 {
			t.Fatalf("%s: %d", method, w.Code)
		}
	}
}

func TestRuntimePodMaintenanceRejectsUnknownAndInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"namespace":"arbitrary","slot_count":1}`, `{"runtime_type":"openclaw","template_pod_id":1,"slot_count":0}`, `null`, `{} {}`} {
		router := gin.New()
		h := NewRuntimePodMaintenanceHandler(nil)
		router.POST("/", func(c *gin.Context) { c.Set("userRole", "admin"); h.Scale(c) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
}

func TestRuntimePodMaintenanceScaleBodyIsStrict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"runtime_type":"hermes","pod_count":1,"template_pod_id":9}`,
		`{"runtime_type":"hermes","pod_count":1,"slot_count":8}`,
		`{"runtime_type":"hermes","pod_count":1,"name":"custom"}`,
		`{"runtime_type":"hermes","pod_count":1,"pod_resources":{}}`,
		`{"runtime_type":"hermes","pod_count":1,"coordinator_resources":{}}`,
		`{"runtime_type":"hermes","pod_count":1,"slot_resources":{}}`,
		`{"runtime_type":"hermes","pod_count":0}`,
		`{"runtime_type":"hermes","pod_count":11}`,
		`{"runtime_type":"hermes","pod_count":1.5}`,
	} {
		router := gin.New()
		h := NewRuntimePodMaintenanceHandler(nil)
		router.POST("/", func(c *gin.Context) { c.Set("userRole", "admin"); h.Scale(c) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
}

func TestRuntimePodMaintenanceAcceptsOnlyRuntimeAndAddedCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewRuntimePodMaintenanceHandler(nil)
	router.POST("/", func(c *gin.Context) { c.Set("userRole", "admin"); h.Scale(c) })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"runtime_type":"hermes","pod_count":2}`)))
	if w.Code != 409 {
		t.Fatalf("valid count did not reach persistent service admission: %d %s", w.Code, w.Body.String())
	}
}
