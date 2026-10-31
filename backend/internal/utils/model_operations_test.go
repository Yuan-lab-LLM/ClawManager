package utils

import (
	"clawreef/internal/models"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestModelOperationErrorsAreNotRetryableServerFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, err := range []error{models.ErrChatNotSupported, fmt.Errorf("%w: incompatible image protocol", models.ErrInvalidModelOperations)} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		HandleError(c, err)
		if w.Code != http.StatusBadRequest {
			t.Fatal(w.Code, w.Body.String())
		}
		if err == models.ErrChatNotSupported && !strings.Contains(w.Body.String(), "model_operation_not_supported") {
			t.Fatal("missing machine-readable code")
		}
	}
}
