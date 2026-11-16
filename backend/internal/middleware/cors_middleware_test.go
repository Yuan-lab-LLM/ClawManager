package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSExposesCreatedInstanceIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodPost, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			router := gin.New()
			router.Use(CORS())
			called := false
			router.POST("/instances", func(c *gin.Context) {
				called = true
				c.Header("X-ClawManager-Created-Instance", "71")
				c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "skill_attach_forbidden"})
			})
			request := httptest.NewRequest(method, "/instances", nil)
			request.Header.Set("Origin", "http://localhost:9002")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if got := response.Header().Get("Access-Control-Expose-Headers"); got != "X-ClawManager-Created-Instance" {
				t.Fatalf("exposed headers = %q, want created instance identity", got)
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("allowed origin changed: %q", got)
			}
			if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization, X-Requested-With" {
				t.Fatalf("allowed headers changed: %q", got)
			}
			wantStatus := http.StatusForbidden
			if method == http.MethodOptions {
				wantStatus = http.StatusNoContent
			}
			if response.Code != wantStatus || called != (method == http.MethodPost) {
				t.Fatalf("status = %d, handler called = %v", response.Code, called)
			}
			if method == http.MethodPost && response.Header().Get("X-ClawManager-Created-Instance") != "71" {
				t.Fatal("created instance identity was lost on setup failure")
			}
		})
	}
}
