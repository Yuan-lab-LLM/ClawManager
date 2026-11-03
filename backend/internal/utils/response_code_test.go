package utils

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

type typedResponseError struct {
	code   string
	status int
}

func (e typedResponseError) Error() string     { return "configuration unavailable" }
func (e typedResponseError) ErrorCode() string { return e.code }
func (e typedResponseError) HTTPStatus() int   { return e.status }
func TestHandleErrorPreservesWrappedMachineCodeAndLegacyMessage(t *testing.T) {
	for _, e := range []typedResponseError{{"MODEL_CONFIGURATION_REQUIRED", 409}, {"PROXY_POLICY_NOT_READY", 503}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		HandleError(c, fmt.Errorf("prepare gateway: %w", e))
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != e.status || result["error_code"] != e.code || result["error"] != "prepare gateway: configuration unavailable" || result["success"] != false {
			t.Fatalf("unexpected response %d %v", w.Code, result)
		}
	}
}
