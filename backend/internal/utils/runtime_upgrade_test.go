package utils

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRuntimeUpgradeErrorsReturnConflict(t *testing.T) {
	for _, err := range []error{models.ErrRuntimeUpgradeInProgress, fmt.Errorf("guard: %w", models.ErrRuntimeUpgradeConflict), models.ErrRuntimeUpgradeWritersActive} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		HandleError(c, err)
		if w.Code != 409 {
			t.Fatalf("%v: %d", err, w.Code)
		}
	}
}
