package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type modelSyncHandlerInstanceService struct {
	services.InstanceService
	instance *models.Instance
	reads    int
}

func (f *modelSyncHandlerInstanceService) GetByID(int) (*models.Instance, error) {
	return f.instance, nil
}
func (f *modelSyncHandlerInstanceService) ModelCatalogSyncStatus(context.Context, *models.Instance) *services.InstanceModelCatalogSyncStatus {
	f.reads++
	return &services.InstanceModelCatalogSyncStatus{State: "applied", Generation: 3, ModelCount: 4}
}

func TestModelCatalogSyncEndpointChecksOwnerBeforeReadingRuntime(t *testing.T) {
	for _, tc := range []struct {
		name  string
		user  int
		role  string
		want  int
		reads int
	}{{"owner", 1, "user", 200, 1}, {"other user", 2, "user", 403, 0}, {"administrator", 2, "admin", 200, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			service := &modelSyncHandlerInstanceService{instance: &models.Instance{ID: 7, UserID: 1, RuntimeGeneration: 3}}
			handler := &InstanceHandler{instanceService: service}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("userID", tc.user); c.Set("userRole", tc.role) })
			router.GET("/instances/:id/model-catalog-sync", handler.GetModelCatalogSync)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/instances/7/model-catalog-sync", nil))
			if response.Code != tc.want || service.reads != tc.reads {
				t.Fatalf("status=%d runtime reads=%d", response.Code, service.reads)
			}
			if tc.want == 403 && strings.Contains(response.Body.String(), "applied") {
				t.Fatal("another owner's model status leaked")
			}
		})
	}
}
