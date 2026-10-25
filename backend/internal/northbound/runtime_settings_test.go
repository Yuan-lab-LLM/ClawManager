package northbound

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type staticRuntimeSettings struct {
	settings *models.NorthboundAdminSettings
	callers  map[int]*models.NorthboundCallerPolicy
}

func (s staticRuntimeSettings) Current() *models.NorthboundAdminSettings {
	copy := *s.settings
	return &copy
}
func (s staticRuntimeSettings) CallerPolicy(userID int) (*models.NorthboundCallerPolicy, error) {
	return s.callers[userID], nil
}

func TestNorthboundEnabledStopsOnlyPublicAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := staticRuntimeSettings{settings: &models.NorthboundAdminSettings{APIEnabled: false}}
	router := gin.New()
	router.Use(RequestContext())
	router.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	api := router.Group("/api/northbound/v1")
	api.Use(NorthboundEnabled(provider))
	api.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/northbound/v1/probe", nil))
	if public.Code != http.StatusServiceUnavailable {
		t.Fatalf("public API status=%d want=%d", public.Code, http.StatusServiceUnavailable)
	}
	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d want=%d", health.Code, http.StatusOK)
	}
}

func TestRuntimeResourceSettingsAreApplied(t *testing.T) {
	settings := &models.NorthboundAdminSettings{LiteCPUCores: 3.5, LiteMemoryGB: 6, LiteDiskGB: 9, ProCPUCores: 7, ProMemoryGB: 18, ProDiskGB: 80, WorkBuddyProCPUCores: 5, WorkBuddyProMemoryGB: 12, WorkBuddyProDiskGB: 60}
	item := &models.NorthboundOperation{OperationID: "op_test"}
	lite := liteCreateRequestWithSettings(item, CreateLiteInstanceRequest{Name: "lite", Type: "openclaw"}, settings)
	if lite.CPUCores != 3.5 || lite.MemoryGB != 6 || lite.DiskGB != 9 {
		t.Fatalf("unexpected Lite resources: %+v", lite)
	}
	workbuddy, err := proCreateRequestWithSettings(item, CreateProInstanceRequest{Name: "pro", Type: "workbuddy"}, settings)
	if err != nil {
		t.Fatal(err)
	}
	if workbuddy.CPUCores != 5 || workbuddy.MemoryGB != 12 || workbuddy.DiskGB != 60 {
		t.Fatalf("unexpected WorkBuddy resources: %+v", workbuddy)
	}
}
