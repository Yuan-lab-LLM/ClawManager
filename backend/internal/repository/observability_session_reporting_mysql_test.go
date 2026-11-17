package repository

import (
	"errors"
	"testing"
	"time"
)

func TestSessionSQLPagingFiltersBeforeLimitAndKeepsGlobalSummary(t *testing.T) {
	s := reportingFixture(t)
	reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	addReportingInvocation(t, s, 1, 9, "trace-a", "agent:openclaw:alpha", "model-a", "completed", now)
	addReportingInvocation(t, s, 2, 9, "trace-b", "sess_trace", "model-b", "failed", now)
	addReportingInvocation(t, s, 3, 9, "trace-b", "sess_trace", "model-b", "completed", now)
	addReportingInvocation(t, s, 4, 9, "trace-blocked", "blocked", "model", "blocked", now)
	for _, q := range []string{
		`INSERT INTO chat_sessions VALUES(1,9,'agent:openclaw:alpha','Research title','2026-11-17 12:00:00')`,
		`INSERT INTO cost_records(trace_id,session_id,instance_id,invocation_id,provider_type,model_name,estimated_cost) VALUES('trace-b','sess_trace',9,NULL,'openai','b',2),('trace-b','sess_trace',9,3,'openai','b',3),('trace-blocked','blocked',9,4,'openai','b',100)`,
		`INSERT INTO audit_events(trace_id,instance_id,event_type,traffic_class,severity,message,created_at) VALUES('fallback',9,'gateway.session.fallback','llm','info','fallback',NOW())`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	result, err := reporter.ListSessionUsageReport(9, 1, 1, "research", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].Title == nil || *result.Items[0].Title != "Research title" {
		t.Fatalf("title search before paging: %+v", result)
	}
	summary := result.Summary
	if summary.SessionCount != 2 || summary.TotalTokens != 45 || summary.EstimatedCost != 5 || summary.FallbackSessionCount != 1 || summary.RecentFallbackAuditCount != 1 || summary.Latest == nil || summary.Latest.ActualProviderModel != "model-b" {
		t.Fatalf("unfiltered summary/count-once fees: %+v", summary)
	}
	result, err = reporter.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{})
	if err != nil || len(result.Items) != 1 || result.Items[0].SessionID != "sess_trace" {
		t.Fatalf("stable session tie ordering: %+v %v", result, err)
	}
	result, err = reporter.ListSessionUsageReport(9, 3, 1, "", SessionUsageFilter{})
	if err != nil || result.Total != 2 || len(result.Items) != 0 {
		t.Fatalf("empty deep page total: %+v %v", result, err)
	}
	result, err = reporter.ListSessionUsageReport(9, 1, 1, "alpha", SessionUsageFilter{})
	if err != nil || result.Total != 1 || result.Items[0].SessionID != "agent:openclaw:alpha" {
		t.Fatalf("session key search: %+v %v", result, err)
	}
	until := now
	result, err = reporter.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{Until: &until})
	if err != nil || result.Total != 0 || result.Summary.SessionCount != 0 || result.Summary.Latest != nil {
		t.Fatalf("exclusive until boundary: %+v %v", result, err)
	}
	for _, search := range []string{"", "research", "missing"} {
		cte, args := sessionUsageReportSQL(9, "", SessionUsageFilter{})
		rows, err := s.SQL().Query("EXPLAIN FORMAT=JSON "+cte+" SELECT session_id FROM sessions WHERE LOCATE(?,LOWER(COALESCE(title,'')))>0 ORDER BY last_seen_at DESC,session_id DESC LIMIT 1 OFFSET 1", append(args, search)...)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		if !rows.Next() {
			t.Fatal("missing execution plan")
		}
		if err = rows.Scan(&plan); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		t.Logf("session EXPLAIN search=%q: %s", search, plan)
	}
}

func TestAdminSessionSQLPagingBatchesManagedScopeAndSearchSummary(t *testing.T) {
	s := reportingFixture(t)
	reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	addReportingInvocation(t, s, 1, 9, "one", "main", "a", "completed", now)
	addReportingInvocation(t, s, 2, 10, "two", "main", "b", "completed", now)
	addReportingInvocation(t, s, 3, 11, "three", "main", "c", "completed", now)
	if _, err := s.SQL().Exec(`INSERT INTO instances VALUES(12,1,'deleted','openclaw','deleted'),(13,1,'idle','workbuddy','error')`); err != nil {
		t.Fatal(err)
	}
	result, err := reporter.ListAdminSessionUsageReport(1, 1, "", SessionUsageFilter{})
	if err != nil || result.Total != 3 || len(result.Items) != 1 || result.Summary.TotalTokens != 30 || result.Items[0].InstanceID != 10 {
		t.Fatalf("managed scope/global summary/stable token ordering: %+v %v", result, err)
	}
	result, err = reporter.ListAdminSessionUsageReport(1, 1, "primary", SessionUsageFilter{})
	if err != nil || result.Total != 1 || result.Summary.TotalTokens != 15 {
		t.Fatalf("instance search must constrain global summary: %+v %v", result, err)
	}
	result, err = reporter.ListAdminSessionUsageReport(2, 1, "primary", SessionUsageFilter{})
	if err != nil || result.Total != 1 || len(result.Items) != 0 || result.Summary.TotalTokens != 15 {
		t.Fatalf("empty admin deep page keeps summary: %+v %v", result, err)
	}
	result, err = reporter.ListAdminSessionUsageReport(1, 1, "missing", SessionUsageFilter{})
	if err != nil || result.Total != 0 || len(result.Items) != 0 || result.Summary.TotalTokens != 0 {
		t.Fatalf("empty admin search: %+v %v", result, err)
	}
}

func TestSessionSQLReportingRejectsMixedCurrencies(t *testing.T) {
	s := reportingFixture(t)
	reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	addReportingInvocation(t, s, 1, 9, "usd", "usd", "a", "completed", now)
	addReportingInvocation(t, s, 2, 10, "eur", "eur", "b", "completed", now)
	if _, err := s.SQL().Exec(`INSERT INTO cost_records(trace_id,session_id,instance_id,invocation_id,provider_type,model_name,estimated_cost,currency) VALUES('usd','usd',9,1,'openai','a',2,'USD'),('eur','eur',10,2,'openai','b',3,'EUR')`); err != nil {
		t.Fatal(err)
	}
	if _, err := reporter.ListAdminSessionUsageReport(1, 1, "", SessionUsageFilter{}); !errors.Is(err, ErrMixedCostCurrencies) {
		t.Fatalf("admin mixed currency error: %v", err)
	}
	result, err := reporter.ListAdminSessionUsageReport(1, 1, "primary", SessionUsageFilter{})
	if err != nil || result.Summary.Currency != "USD" || result.Summary.EstimatedCost != 2 {
		t.Fatalf("search scoped currency: %+v %v", result, err)
	}
	if _, err := s.SQL().Exec(`UPDATE cost_records SET instance_id=9,session_id='usd',invocation_id=1 WHERE currency='EUR'`); err != nil {
		t.Fatal(err)
	}
	if _, err := reporter.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{}); !errors.Is(err, ErrMixedCostCurrencies) {
		t.Fatalf("session mixed currency error: %v", err)
	}
}

func TestSessionSQLReportingRetainsBoundedStreamRetryMetadata(t *testing.T) {
	s := reportingFixture(t)
	reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
	now := time.Now().UTC().Truncate(time.Second)
	addReportingInvocation(t, s, 1, 9, "retry", "main", "first", "failed", now.Add(-time.Second))
	if _, err := s.SQL().Exec(`UPDATE model_invocations SET is_streaming=TRUE,latency_ms=100,ttft_ms=10 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SQL().Exec(`INSERT INTO audit_events(trace_id,request_id,instance_id,event_type,traffic_class,severity,message,details,created_at) VALUES('retry','retry-request',9,'gateway.deployment.failover','llm','info','retry','{"failed_attempt":1,"next_model":"replacement"}',?)`, now); err != nil {
		t.Fatal(err)
	}
	result, err := reporter.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	latest := result.Summary.Latest
	if latest == nil || latest.Status != "retrying" || latest.ActualProviderModel != "replacement" || latest.LatencyMs != nil || latest.TTFTMs != nil || !latest.CreatedAt.Equal(now) {
		t.Fatalf("retry metadata: %+v", latest)
	}
	until := now.Add(time.Hour)
	result, err = reporter.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{Until: &until})
	if err != nil || result.Summary.Latest.Status != "failed" {
		t.Fatalf("historical view must retain invocation status: %+v %v", result, err)
	}
}

func TestSessionSQLReportingRejectsInvalidPages(t *testing.T) {
	s := reportingFixture(t)
	reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
	for _, p := range [][2]int{{0, 1}, {1, 0}, {1, 101}, {int(^uint(0) >> 1), 100}} {
		if _, err := reporter.ListSessionUsageReport(9, p[0], p[1], "", SessionUsageFilter{}); err == nil {
			t.Fatalf("invalid session page accepted: %v", p)
		}
		if _, err := reporter.ListAdminSessionUsageReport(p[0], p[1], "", SessionUsageFilter{}); err == nil {
			t.Fatalf("invalid admin page accepted: %v", p)
		}
	}
}

func TestSessionSQLReportingReadsOnlyExactSession(t *testing.T) {
	s := reportingFixture(t)
	reporter, ok := NewModelInvocationRepository(s).(interface {
		GetSessionUsageReportItem(int, string, SessionUsageFilter) (*SessionUsageReportItem, error)
	})
	if !ok {
		t.Fatal("bounded exact-session SQL capability missing; detail still materializes every session")
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	addReportingInvocation(t, s, 1, 9, "exact", "exact", "a", "completed", now)
	addReportingInvocation(t, s, 2, 9, "neighbor", "neighbor", "b", "completed", now)
	addReportingInvocation(t, s, 3, 9, "exact-old", "exact", "c", "completed", now.Add(-time.Hour))
	if _, err := s.SQL().Exec(`INSERT INTO cost_records(trace_id,session_id,instance_id,invocation_id,provider_type,model_name,estimated_cost) VALUES('exact','exact',9,1,'openai','a',2),('neighbor','neighbor',9,2,'openai','b',100),('exact-old','exact',9,3,'openai','c',20)`); err != nil {
		t.Fatal(err)
	}
	item, err := reporter.GetSessionUsageReportItem(9, "exact", SessionUsageFilter{Since: &now})
	if err != nil || item == nil || item.SessionID != "exact" || item.TotalTokens != 15 || item.InvocationCount != 1 || item.EstimatedCost != 2 {
		t.Fatalf("exact session/time window must exclude neighbor/old rows: %+v %v", item, err)
	}
	item, err = reporter.GetSessionUsageReportItem(9, "missing", SessionUsageFilter{})
	if err != nil || item != nil {
		t.Fatalf("missing session must be nil: %+v %v", item, err)
	}
	item, err = reporter.GetSessionUsageReportItem(10, "exact", SessionUsageFilter{})
	if err != nil || item != nil {
		t.Fatalf("instance scope must be exact: %+v %v", item, err)
	}
}
