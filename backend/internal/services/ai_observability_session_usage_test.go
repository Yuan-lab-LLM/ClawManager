package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type stubSessionUsageInvocationRepo struct {
	aggregates []repository.InstanceSessionTokenAggregate
	invocations []models.ModelInvocation
}

func (s *stubSessionUsageInvocationRepo) Create(*models.ModelInvocation) error { return nil }
func (s *stubSessionUsageInvocationRepo) GetByID(int) (*models.ModelInvocation, error) {
	return nil, nil
}
func (s *stubSessionUsageInvocationRepo) ListByTraceID(string) ([]models.ModelInvocation, error) {
	return nil, nil
}
func (s *stubSessionUsageInvocationRepo) ListBySessionID(string, int) ([]models.ModelInvocation, error) {
	return nil, nil
}
func (s *stubSessionUsageInvocationRepo) ListByUserID(int, int) ([]models.ModelInvocation, error) {
	return nil, nil
}
func (s *stubSessionUsageInvocationRepo) ListRecent(int) ([]models.ModelInvocation, error) {
	return nil, nil
}
func (s *stubSessionUsageInvocationRepo) AggregateByInstanceSession(int, repository.SessionUsageFilter) ([]repository.InstanceSessionTokenAggregate, error) {
	return s.aggregates, nil
}
func (s *stubSessionUsageInvocationRepo) ListRecentByInstanceSession(int, string, int, repository.SessionUsageFilter) ([]models.ModelInvocation, error) {
	return s.invocations, nil
}
func (s *stubSessionUsageInvocationRepo) CountDistinctSessionsByInstance(int, repository.SessionUsageFilter) (int, error) {
	return len(s.aggregates), nil
}

type stubSessionUsageCostRepo struct {
	aggregates []repository.InstanceSessionCostAggregate
	byTraceID  map[string][]models.CostRecord
}

func (s *stubSessionUsageCostRepo) Create(*models.CostRecord) error { return nil }
func (s *stubSessionUsageCostRepo) ListByTraceID(traceID string) ([]models.CostRecord, error) {
	if s.byTraceID == nil {
		return nil, nil
	}
	return s.byTraceID[traceID], nil
}
func (s *stubSessionUsageCostRepo) ListByUserID(int, int) ([]models.CostRecord, error) {
	return nil, nil
}
func (s *stubSessionUsageCostRepo) ListRecent(int) ([]models.CostRecord, error) { return nil, nil }
func (s *stubSessionUsageCostRepo) AggregateCostByInstanceSession(int, repository.SessionUsageFilter) ([]repository.InstanceSessionCostAggregate, error) {
	return s.aggregates, nil
}

type stubSessionUsageChatSessionRepo struct {
	sessions []models.ChatSession
}

func (s *stubSessionUsageChatSessionRepo) GetBySessionID(string) (*models.ChatSession, error) {
	return nil, nil
}
func (s *stubSessionUsageChatSessionRepo) ListByInstanceID(int) ([]models.ChatSession, error) {
	return s.sessions, nil
}
func (s *stubSessionUsageChatSessionRepo) Save(*models.ChatSession) error { return nil }

type stubSessionUsageAuditRepo struct {
	counts map[string]int
}

func (s *stubSessionUsageAuditRepo) Create(*models.AuditEvent) error { return nil }
func (s *stubSessionUsageAuditRepo) ListByTraceID(string) ([]models.AuditEvent, error) {
	return nil, nil
}
func (s *stubSessionUsageAuditRepo) ListRecent(int) ([]models.AuditEvent, error) {
	return nil, nil
}
func (s *stubSessionUsageAuditRepo) CountRecentByInstanceAndEventType(instanceID int, eventType string, since time.Time) (int, error) {
	if s.counts == nil {
		return 0, nil
	}
	return s.counts[eventType], nil
}

func TestGetInstanceSessionUsageMergesInvocationCostAndSessionMetadata(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	title := "Weather chat"
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{
					SessionID:        "agent:openclaw:main",
					PromptTokens:     100,
					CompletionTokens: 40,
					TotalTokens:      140,
					InvocationCount:  2,
					FirstSeenAt:        now.Add(-time.Hour),
					LastSeenAt:         now,
				},
			},
		},
		costRepo: &stubSessionUsageCostRepo{
			aggregates: []repository.InstanceSessionCostAggregate{
				{
					SessionID:     "agent:openclaw:main",
					EstimatedCost: 0.12,
					Currency:      "USD",
				},
			},
		},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{
			sessions: []models.ChatSession{
				{SessionID: "agent:openclaw:main", Title: &title},
			},
		},
	}

	result, err := service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("GetInstanceSessionUsage failed: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	item := result.Items[0]
	if item.SessionKey != "main" || item.TotalTokens != 140 || item.EstimatedCost != 0.12 {
		t.Fatalf("unexpected merged item: %+v", item)
	}
	if item.Title == nil || *item.Title != title {
		t.Fatalf("expected title merge, got %+v", item.Title)
	}
}

func TestGetInstanceSessionUsageSearchFiltersBySessionKey(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 10, LastSeenAt: now},
				{SessionID: "agent:openclaw:research", TotalTokens: 20, LastSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
	}

	result, err := service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{Page: 1, Limit: 10, Search: "research"})
	if err != nil {
		t.Fatalf("GetInstanceSessionUsage failed: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].SessionKey != "research" {
		t.Fatalf("unexpected filtered items: %+v", result.Items)
	}
	if result.Summary.SessionCount != 2 || result.Summary.TotalTokens != 30 {
		t.Fatalf("summary should ignore search filter, got %+v", result.Summary)
	}
}

func TestGetInstanceSessionUsageComplianceCountsFallbackSessions(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 10, LastSeenAt: now},
				{SessionID: "sess_trc_abc", TotalTokens: 5, LastSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
	}

	result, err := service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("GetInstanceSessionUsage failed: %v", err)
	}
	if !result.Compliance.HasFallbackSessions || result.Compliance.FallbackSessionCount != 1 {
		t.Fatalf("unexpected compliance: %+v", result.Compliance)
	}
}

func TestGetInstanceSessionUsageComplianceIncludesFallbackAuditCount(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 10, LastSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		auditRepo: &stubSessionUsageAuditRepo{
			counts: map[string]int{"gateway.session.fallback": 3},
		},
	}

	result, err := service.GetInstanceSessionUsage(9, InstanceSessionUsageQuery{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("GetInstanceSessionUsage failed: %v", err)
	}
	if result.Compliance.RecentFallbackAuditCount != 3 {
		t.Fatalf("expected recent fallback audit count 3, got %+v", result.Compliance)
	}
}

func TestGetInstanceSessionUsageDetailBuildsModelBreakdown(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{
					SessionID:    "agent:openclaw:main",
					TotalTokens:  30,
					LastSeenAt:   now,
					FirstSeenAt:  now,
					InvocationCount: 1,
				},
			},
			invocations: []models.ModelInvocation{
				{
					TraceID:        "trc_1",
					RequestedModel: "auto",
					Status:         models.ModelInvocationStatusCompleted,
					PromptTokens:   20,
					CompletionTokens: 10,
					TotalTokens:    30,
					CreatedAt:      now,
				},
			},
		},
		costRepo: &stubSessionUsageCostRepo{
			byTraceID: map[string][]models.CostRecord{
				"trc_1": {
					{TraceID: "trc_1", EstimatedCost: 0.05, Currency: "USD"},
				},
			},
		},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		llmModelRepo:    &stubLLMModelRepository{},
	}

	detail, err := service.GetInstanceSessionUsageDetail(9, "agent:openclaw:main", repository.SessionUsageFilter{})
	if err != nil {
		t.Fatalf("GetInstanceSessionUsageDetail failed: %v", err)
	}
	if len(detail.ModelBreakdown) != 1 || detail.ModelBreakdown[0].Label != "auto" {
		t.Fatalf("unexpected model breakdown: %+v", detail.ModelBreakdown)
	}
	if detail.ModelBreakdown[0].EstimatedCost != 0.05 {
		t.Fatalf("expected model breakdown cost 0.05, got %+v", detail.ModelBreakdown[0])
	}
	if len(detail.RecentTraces) != 1 || detail.RecentTraces[0].TraceID != "trc_1" {
		t.Fatalf("unexpected recent traces: %+v", detail.RecentTraces)
	}
}

func TestGetInstanceLLMGovernanceStatusUnknownConfigUsesFallbackOnly(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 10, LastSeenAt: now, FirstSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		auditRepo: &stubSessionUsageAuditRepo{
			counts: map[string]int{"egress.llm.blocked": 2},
		},
	}

	status, err := service.GetInstanceLLMGovernanceStatus(9, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetInstanceLLMGovernanceStatus failed: %v", err)
	}
	if !status.IsCompliant || status.ConfigStatus != "unknown" || status.RecentEgressBlockCount != 2 {
		t.Fatalf("unexpected governance status: %+v", status)
	}
}

func TestGetInstanceLLMGovernanceStatusExternalConfigIsNonCompliant(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 10, LastSeenAt: now, FirstSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
	}

	status, err := service.GetInstanceLLMGovernanceStatus(9, map[string]interface{}{
		"llm_config_status": "external",
	})
	if err != nil {
		t.Fatalf("GetInstanceLLMGovernanceStatus failed: %v", err)
	}
	if status.IsCompliant {
		t.Fatalf("expected external config to be non-compliant")
	}
}

func TestGetAdminSessionUsageOverviewAggregatesManagedInstances(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 100, LastSeenAt: now, FirstSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		instanceRepo: &stubGovernanceInstanceRepo{
			instances: []models.Instance{
				{ID: 1, UserID: 9, Name: "oc-1", Type: "openclaw", Status: "running"},
				{ID: 2, UserID: 10, Name: "ubuntu", Type: "ubuntu", Status: "running"},
			},
		},
	}

	overview, err := service.GetAdminSessionUsageOverview(InstanceSessionUsageOverviewQuery{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("GetAdminSessionUsageOverview failed: %v", err)
	}
	if overview.Total != 1 || len(overview.Items) != 1 {
		t.Fatalf("expected 1 managed instance item, got total=%d items=%d", overview.Total, len(overview.Items))
	}
	if overview.Summary.TotalTokens != 100 {
		t.Fatalf("unexpected global summary: %+v", overview.Summary)
	}
}

func TestGetAdminSessionUsageOverviewFiltersBySearch(t *testing.T) {
	now := time.Now().UTC()
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 100, LastSeenAt: now, FirstSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		instanceRepo: &stubGovernanceInstanceRepo{
			instances: []models.Instance{
				{ID: 1, UserID: 9, Name: "alpha-openclaw", Type: "openclaw", Status: "running"},
				{ID: 2, UserID: 9, Name: "beta-openclaw", Type: "openclaw", Status: "running"},
			},
		},
	}

	overview, err := service.GetAdminSessionUsageOverview(InstanceSessionUsageOverviewQuery{
		Page:   1,
		Limit:  10,
		Search: "alpha",
	})
	if err != nil {
		t.Fatalf("GetAdminSessionUsageOverview failed: %v", err)
	}
	if overview.Total != 1 || len(overview.Items) != 1 || overview.Items[0].InstanceName != "alpha-openclaw" {
		t.Fatalf("unexpected filtered overview: total=%d items=%+v", overview.Total, overview.Items)
	}
}

func TestGetAdminSessionUsageOverviewAcceptsSinceQuery(t *testing.T) {
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	service := &aiObservabilityService{
		invocationRepo: &stubSessionUsageInvocationRepo{
			aggregates: []repository.InstanceSessionTokenAggregate{
				{SessionID: "agent:openclaw:main", TotalTokens: 50, LastSeenAt: now, FirstSeenAt: now},
			},
		},
		costRepo:        &stubSessionUsageCostRepo{},
		chatSessionRepo: &stubSessionUsageChatSessionRepo{},
		instanceRepo: &stubGovernanceInstanceRepo{
			instances: []models.Instance{
				{ID: 1, UserID: 9, Name: "oc-1", Type: "openclaw", Status: "running"},
			},
		},
	}

	overview, err := service.GetAdminSessionUsageOverview(InstanceSessionUsageOverviewQuery{
		Page:  1,
		Limit: 10,
		Since: &since,
	})
	if err != nil {
		t.Fatalf("GetAdminSessionUsageOverview failed: %v", err)
	}
	if overview.Total != 1 || overview.Summary.TotalTokens != 50 {
		t.Fatalf("unexpected since-filtered overview: total=%d summary=%+v", overview.Total, overview.Summary)
	}
}
