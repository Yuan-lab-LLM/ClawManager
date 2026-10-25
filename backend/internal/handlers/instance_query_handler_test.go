package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"

	"github.com/gin-gonic/gin"
)

type queryRecordingInstanceService struct {
	fakeWorkspaceHandlerInstanceService
	userID  int
	filter  models.InstanceListFilter
	offset  int
	limit   int
	items   []models.Instance
	total   int
	summary *models.InstanceSummary
}

func (s *queryRecordingInstanceService) GetFilteredByUserID(userID int, filter models.InstanceListFilter, offset, limit int) ([]models.Instance, int, error) {
	s.userID = userID
	s.filter = filter
	s.offset = offset
	s.limit = limit
	return s.items, s.total, nil
}

func (s *queryRecordingInstanceService) GetSummaryByUserID(userID int) (*models.InstanceSummary, error) {
	s.userID = userID
	return s.summary, nil
}

func TestListInstancesPassesCallerScopedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &queryRecordingInstanceService{
		items: []models.Instance{{ID: 9, UserID: 17, Name: "filtered"}},
		total: 201,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances?page=3&limit=100&query=team-a&type=opencode&instance_mode=lite&availability=available", nil)
	c.Set("userID", 17)

	(&InstanceHandler{instanceService: service}).ListInstances(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.userID != 17 || service.offset != 200 || service.limit != 100 {
		t.Fatalf("query scope = user %d offset %d limit %d", service.userID, service.offset, service.limit)
	}
	if service.filter.Query != "team-a" || service.filter.Type != "opencode" || service.filter.InstanceMode != "lite" || service.filter.Availability != "available" {
		t.Fatalf("unexpected filter: %#v", service.filter)
	}
	var response struct {
		Data struct {
			Total int `json:"total"`
			Page  int `json:"page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.Total != 201 || response.Data.Page != 3 {
		t.Fatalf("unexpected response data: %#v", response.Data)
	}
}

func TestGetInstanceSummaryUsesCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &queryRecordingInstanceService{
		summary: &models.InstanceSummary{Total: 313, Running: 300, AllocatedStorageGB: 6260},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/summary", nil)
	c.Set("userID", 23)

	(&InstanceHandler{instanceService: service}).GetInstanceSummary(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.userID != 23 {
		t.Fatalf("summary user = %d, want 23", service.userID)
	}
	var response struct {
		Data models.InstanceSummary `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.Total != 313 || response.Data.Running != 300 || response.Data.AllocatedStorageGB != 6260 {
		t.Fatalf("unexpected summary: %#v", response.Data)
	}
}
