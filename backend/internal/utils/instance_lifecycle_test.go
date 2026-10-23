package utils

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestLifecycleConflictReturns409WhenWrapped(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	HandleError(c, fmt.Errorf("restart refused: %w", models.ErrInstanceOperationInProgress))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}
