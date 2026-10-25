package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestVersionHandlerReturnsRunningBuildInformation(t *testing.T) {
	t.Setenv("CLAWMANAGER_VERSION", "v2026.9.1")
	t.Setenv("CLAWMANAGER_COMMIT", "abcdef1234567890")
	t.Setenv("CLAWMANAGER_BUILD_TIME", "2026-09-01T08:00:00Z")
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	NewVersionHandler().Get(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Version   string `json:"version"`
			Commit    string `json:"commit"`
			BuildTime string `json:"build_time"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success || response.Data.Version != "v2026.9.1" || response.Data.Commit != "abcdef1234567890" || response.Data.BuildTime != "2026-09-01T08:00:00Z" {
		t.Fatalf("response = %+v, want running build information", response)
	}
}
