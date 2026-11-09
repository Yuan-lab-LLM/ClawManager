package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEgressAccessLoggerDoesNotRecordPreviewGrants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	handler := NewEgressProxyHandler(nil)
	router := gin.New()
	router.Use(handler.AccessLogger(&output))
	router.NoRoute(func(c *gin.Context) { c.Status(http.StatusForbidden) })
	for _, path := range []string{
		"http://clawmanager-team-preview.invalid/v3/static/12/_/grant-secret-sentinel/index.html",
		"http://p-grant.clawmanager-team-preview.invalid/v3/interactive/12/_/grant-secret-sentinel/index.html",
		"/api/v1/build",
	} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if strings.Contains(output.String(), "grant-secret-sentinel") || !strings.Contains(output.String(), "/api/v1/build") {
		t.Fatal("preview grant leaked or ordinary access logs missing")
	}
}
