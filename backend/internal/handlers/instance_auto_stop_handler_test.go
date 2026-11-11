package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAutoStopUpdateRejectsInvalidDurationsBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/", NewInstanceAutoStopHandler(nil).Update)
	for _, body := range []string{
		`{}`, `{ "enabled":true,"version":1,"check_interval_seconds":1 }`,
		`{ "enabled":true,"version":1,"check_interval_seconds":0,"idle_timeout_seconds":1 }`,
		`{ "enabled":true,"version":1,"check_interval_seconds":1,"idle_timeout_seconds":-1 }`,
		`{ "enabled":true,"version":1,"check_interval_seconds":9223372037,"idle_timeout_seconds":1 }`,
		`{ "enabled":true,"version":1,"check_interval_seconds":1.5,"idle_timeout_seconds":1 }`,
		`{ "enabled":true,"version":1,"check_interval_days":0,"idle_timeout_days":1 }`,
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("accepted %s: %d %s", body, response.Code, response.Body.String())
		}
	}
}
