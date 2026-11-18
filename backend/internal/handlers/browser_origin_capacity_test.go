package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/workbench"
	"github.com/gin-gonic/gin"
)

type capacityWorkbenchBinding struct {
	binding *models.InstanceRuntimeBinding
}

func (s capacityWorkbenchBinding) GetRunningByInstanceID(context.Context, int) (*models.InstanceRuntimeBinding, error) {
	return s.binding, nil
}

type capacityWorkbenchPod struct{ pod *models.RuntimePod }

func (s capacityWorkbenchPod) GetByID(context.Context, int64) (*models.RuntimePod, error) {
	return s.pod, nil
}

type capacityWorkbenchAgent struct{}

func (capacityWorkbenchAgent) Workbench(context.Context, string, services.WorkbenchRequest) (json.RawMessage, error) {
	return json.RawMessage(`{"preview":true}`), nil
}

type capacityPreviewStore struct{ puts int }

func (s *capacityPreviewStore) Put(context.Context, string, workbench.PreviewLease) error {
	s.puts++
	return nil
}
func (*capacityPreviewStore) Get(context.Context, string) (workbench.PreviewLease, error) {
	return workbench.PreviewLease{}, workbench.ErrPreviewNotFound
}

func TestBrowserPreviewCapacityResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	endpoint := "http://runtime.test"
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
	binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, WorkspacePath: "/workspace"}
	store := &capacityPreviewStore{}
	previews, err := workbench.NewPreviews("http://p-{token}.legacy.test", store, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := services.NewInstanceWorkbenchService(&fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{42: instance}}, capacityWorkbenchBinding{binding}, capacityWorkbenchPod{&models.RuntimePod{ID: 9, AgentEndpoint: &endpoint}}, capacityWorkbenchAgent{}, previews)
	origins, err := browserorigin.New("https://cm.test:30443", "31400-31401", &fullBrowserOriginStore{})
	if err != nil {
		t.Fatal(err)
	}
	service.SetBrowserOrigins(origins)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", 7) })
	NewInstanceWorkbenchHandler(service).Register(router.Group("/instances/:id/workbench"))
	request := httptest.NewRequest("POST", "/instances/42/workbench/previews?generation=3", strings.NewReader(`{"url":"http://localhost:4321/"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assertBrowserCapacityResponse(t, response)
	if store.puts != 0 {
		t.Fatal("capacity failure created preview capability")
	}
}
