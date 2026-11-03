package handlers

import (
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type adminSummaryFake struct {
	fakeWorkspaceHandlerInstanceService
}

func (s *adminSummaryFake) GetAdminSummary() (*models.InstanceSummary, error) {
	return &models.InstanceSummary{Total: 2501, Running: 2005, AllocatedStorageGB: 12505}, nil
}
func TestAdminInstanceSummaryNotLimitedToPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{"admin", "user"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userRole", role)
		h := &InstanceHandler{instanceService: &adminSummaryFake{}}
		h.GetAdminInstanceSummary(c)
		if role == "admin" {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"running":2005`) {
				t.Fatal(w.Code, w.Body.String())
			}
		} else if w.Code != 403 {
			t.Fatal("non-admin summary access")
		}
	}
}
