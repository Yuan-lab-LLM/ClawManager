package services

import (
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type stubSQLSessionReporter struct {
	stubSessionUsageInvocationRepo
	sessionCalls, adminCalls int
	page, limit              int
	search                   string
	filter                   repository.SessionUsageFilter
	err                      error
}

func (s *stubSQLSessionReporter) AggregateByInstanceSession(int, repository.SessionUsageFilter) ([]repository.InstanceSessionTokenAggregate, error) {
	return nil, errors.New("legacy full-history aggregation called")
}
func (s *stubSQLSessionReporter) ListSessionUsageReport(_ int, page, limit int, search string, filter repository.SessionUsageFilter) (*repository.SessionUsageReport, error) {
	s.sessionCalls++
	s.page, s.limit, s.search, s.filter = page, limit, search, filter
	if s.err != nil {
		return nil, s.err
	}
	return &repository.SessionUsageReport{Total: 37, Summary: repository.SessionUsageReportSummary{TotalTokens: 900, SessionCount: 37, Currency: "USD", FallbackSessionCount: 2, RecentFallbackAuditCount: 3, Latest: &models.ModelInvocation{ActualProviderModel: "recent", Status: "completed"}}, Items: []repository.SessionUsageReportItem{{SessionID: "agent:openclaw:main", TotalTokens: 15, Currency: "USD"}}}, nil
}
func (s *stubSQLSessionReporter) ListAdminSessionUsageReport(page, limit int, search string, filter repository.SessionUsageFilter) (*repository.AdminSessionUsageReport, error) {
	s.adminCalls++
	s.page, s.limit, s.search, s.filter = page, limit, search, filter
	if s.err != nil {
		return nil, s.err
	}
	return &repository.AdminSessionUsageReport{Total: 57, Summary: repository.SessionUsageReportSummary{TotalTokens: 1000, Currency: "USD"}, Items: []repository.AdminSessionUsageReportItem{{InstanceID: 9, InstanceName: "primary", InstanceType: "openclaw", Summary: repository.SessionUsageReportSummary{TotalTokens: 15, Currency: "USD", FallbackSessionCount: 1}}}}, nil
}

func TestSessionUsageUsesSQLPageWithoutLegacyFullHistoryReads(t *testing.T) {
	reporter := &stubSQLSessionReporter{}
	service := &aiObservabilityService{invocationRepo: reporter}
	since := time.Now().UTC()
	result, err := service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{Page: 2, Limit: 1, Search: "main", Since: &since})
	if err != nil {
		t.Fatal(err)
	}
	if reporter.sessionCalls != 1 || reporter.page != 2 || reporter.limit != 1 || reporter.search != "main" || reporter.filter.Since != &since || result.Total != 37 || len(result.Items) != 1 || result.Items[0].SessionKey != "main" || result.Summary.TotalTokens != 900 || result.Summary.LatestActualModel != "recent" || result.Compliance.RecentFallbackAuditCount != 3 || !result.Compliance.HasFallbackSessions {
		t.Fatalf("SQL session routing/mapping failed: reporter=%+v result=%+v", reporter, result)
	}
}

func TestAdminSessionUsageUsesOneSQLBatchWithoutInstanceHydration(t *testing.T) {
	reporter := &stubSQLSessionReporter{}
	service := &aiObservabilityService{invocationRepo: reporter, instanceRepo: &stubGovernanceInstanceRepo{}}
	result, err := service.GetAdminSessionUsageOverview(InstanceSessionUsageOverviewQuery{Page: 3, Limit: 1, Search: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	if reporter.adminCalls != 1 || reporter.page != 3 || reporter.limit != 1 || reporter.search != "primary" || result.Total != 57 || result.Summary.TotalTokens != 1000 || len(result.Items) != 1 || result.Items[0].Summary.TotalTokens != 15 || !result.Items[0].Compliance.HasFallbackSessions {
		t.Fatalf("SQL admin routing/mapping failed: reporter=%+v result=%+v", reporter, result)
	}
	reporter.err = repository.ErrMixedCostCurrencies
	if _, err = service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{}); !errors.Is(err, repository.ErrMixedCostCurrencies) {
		t.Fatalf("session must propagate currency error: %v", err)
	}
	if _, err = service.GetAdminSessionUsageOverview(InstanceSessionUsageOverviewQuery{}); !errors.Is(err, repository.ErrMixedCostCurrencies) {
		t.Fatalf("admin must propagate currency error: %v", err)
	}
}
