package handlers

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestAdminDashboardRejectsNonAdminBeforeReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{"", "user"} {
		r := gin.New()
		h := NewAdminDashboardHandler(nil)
		r.GET("/", func(c *gin.Context) { c.Set("userRole", role) }, h.Get)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 403 {
			t.Fatalf("role %q got %d", role, w.Code)
		}
	}
}

func TestAdminDashboardReturnsNoStoreAndNullableData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAdminDashboardHandler(nil)
	r.GET("/", func(c *gin.Context) { c.Set("userRole", "admin") }, h.Get)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response %d %v", w.Code, w.Header())
	}
	var body struct {
		Data struct {
			TotalUsers *int64 `json:"total_users"`
			Coverage   map[string]struct {
				Status string `json:"status"`
			} `json:"coverage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.TotalUsers != nil || body.Data.Coverage["usage"].Status != "unavailable" {
		t.Fatalf("body %s", w.Body.String())
	}
}
