package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"clawreef/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestAccessLoggerRedactsRequestSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, logger := range []struct {
		name        string
		make        func(*bytes.Buffer) gin.HandlerFunc
		skipPreview bool
	}{
		{"generic", func(_ *bytes.Buffer) gin.HandlerFunc { return SafeAccessLogger() }, false},
		{"egress", func(output *bytes.Buffer) gin.HandlerFunc { return NewEgressProxyHandler(nil).AccessLogger(output) }, true},
	} {
		t.Run(logger.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, target, loggedPath string
				status                   int
			}{
				{"websocket auth failure", "/api/v1/ws?token=jwt-secret-sentinel", "/api/v1/ws", http.StatusUnauthorized},
				{"ordinary query", "/api/v1/build?search=query-secret-sentinel", "/api/v1/build", http.StatusOK},
				{"share root", "/s/share-secret-sentinel?token=query-secret-sentinel", "/s/[redacted]", http.StatusOK},
				{"share tail", "/s/share-secret-sentinel/tail-secret-sentinel?token=query-secret-sentinel", "/s/[redacted]", http.StatusOK},
				{"share no route", "/s/share-secret-sentinel/tail-secret-sentinel?token=query-secret-sentinel", "/s/[redacted]", http.StatusNotFound},
				{"preview no route", "/__clawmanager_preview/preview-secret-sentinel/tail-secret-sentinel?token=query-secret-sentinel", "/__clawmanager_preview/[redacted]", http.StatusNotFound},
				{"ordinary no route", "/missing?token=query-secret-sentinel", "/missing", http.StatusNotFound},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var output bytes.Buffer
					previousWriter := gin.DefaultWriter
					gin.DefaultWriter = &output
					t.Cleanup(func() { gin.DefaultWriter = previousWriter })
					router := gin.New()
					router.Use(BrowserOriginRoutes(nil, nil, nil), logger.make(&output))
					router.GET("/api/v1/ws", middleware.Auth(), NewWebSocketHandler(nil).HandleWebSocket)
					respond := func(c *gin.Context) {
						_ = c.Error(errors.New("error-secret-sentinel"))
						c.Status(http.StatusOK)
					}
					router.GET("/api/v1/build", respond)
					// Exercise fallback logging without any registered share route, too.
					if tc.name != "share no route" {
						router.GET("/s/:code", respond)
						router.GET("/s/:code/*path", respond)
					}
					req := httptest.NewRequest(http.MethodGet, tc.target, strings.NewReader("body-secret-sentinel"))
					req.Header.Set("Cookie", "session=cookie-secret-sentinel")
					req.Header.Set("Referer", "https://example.invalid/referrer-secret-sentinel")
					if tc.name != "websocket auth failure" {
						req.Header.Set("Authorization", "Bearer auth-secret-sentinel")
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, req)
					if response.Code != tc.status {
						t.Fatalf("status = %d, want %d", response.Code, tc.status)
					}
					if req.URL.RequestURI() != tc.target {
						t.Fatal("access logger changed request URL")
					}
					logged := output.String()
					if strings.Contains(logged, "secret-sentinel") || strings.Contains(logged, "?") {
						t.Error("request secrets or query string entered access log")
					}
					if logger.skipPreview && tc.name == "preview no route" {
						if logged != "" {
							t.Error("egress preview skip changed")
						}
						return
					}
					if !strings.Contains(logged, tc.loggedPath) || !strings.Contains(logged, "GET") || !strings.Contains(logged, strconv.Itoa(tc.status)) {
						t.Error("access log lost request path, method, or status")
					}
				})
			}
		})
	}
}
