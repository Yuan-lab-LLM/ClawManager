package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/repository"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

type stubAIObservabilityHandlerService struct {
	overview *services.InstanceSessionUsageOverview
	err      error
	lastQuery services.InstanceSessionUsageOverviewQuery
}

func (s *stubAIObservabilityHandlerService) ListAuditItems(services.AuditQuery) (*services.AuditListResult, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetTraceDetail(string) (*services.AuditTraceDetail, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetCostOverview(services.CostQuery) (*services.CostOverview, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetInstanceSessionUsage(int, services.InstanceSessionUsageQuery) (*services.InstanceSessionUsageResult, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetInstanceSessionUsageDetail(int, string, repository.SessionUsageFilter) (*services.InstanceSessionUsageDetail, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetInstanceLLMGovernanceStatus(int, map[string]interface{}) (*services.InstanceLLMGovernanceStatus, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetLLMGovernanceOverview() (*services.LLMGovernanceOverview, error) {
	return nil, nil
}
func (s *stubAIObservabilityHandlerService) GetAdminSessionUsageOverview(query services.InstanceSessionUsageOverviewQuery) (*services.InstanceSessionUsageOverview, error) {
	s.lastQuery = query
	if s.err != nil {
		return nil, s.err
	}
	return s.overview, nil
}

func TestGetSessionUsageOverviewReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/session-usage/overview?page=1&limit=10&search=oc", nil)

	service := &stubAIObservabilityHandlerService{
		overview: &services.InstanceSessionUsageOverview{
			Summary: services.InstanceSessionUsageSummary{Currency: "USD"},
			Items:   []services.InstanceSessionUsageOverviewItem{},
			Total:   0,
			Page:    1,
			Limit:   10,
		},
	}
	handler := NewAIObservabilityHandler(service)
	handler.GetSessionUsageOverview(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.lastQuery.Search != "oc" {
		t.Fatalf("expected search=oc, got %q", service.lastQuery.Search)
	}
	if !strings.Contains(recorder.Body.String(), "Session usage overview retrieved successfully") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestGetSessionUsageOverviewInvalidSince(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/session-usage/overview?since=not-a-date", nil)

	handler := NewAIObservabilityHandler(&stubAIObservabilityHandlerService{})
	handler.GetSessionUsageOverview(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestGetSessionUsageOverviewRejectsUntilBeforeSince(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/session-usage/overview?since=2026-07-10T00:00:00Z&until=2026-07-01T00:00:00Z", nil)

	handler := NewAIObservabilityHandler(&stubAIObservabilityHandlerService{})
	handler.GetSessionUsageOverview(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}
