package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

// Keep the original single-predicate SQL as an independent semantic oracle for
// the optimized count: overlapping fields count once, including absent owners.
func TestAuditSearchCountMatchesOriginalSubstringPredicate(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	execute := func(query string, args ...any) {
		t.Helper()
		if _, err := s.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	execute(`INSERT INTO users(id,username,email,password_hash) VALUES(9101,'OwnerToken Shared','audit-search-1@example.test','fixture'),(9102,'OtherOwner','audit-search-2@example.test','fixture')`)
	now := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	owner := 9101
	calls := []*models.ModelInvocation{
		{TraceID: "shared-trace", RequestID: "shared-request", RequestedModel: "Shared", ActualProviderModel: "shared-model", UserID: &owner},
		{TraceID: "trace-token", RequestID: "request-token", RequestedModel: "AUTO", ActualProviderModel: "Ñandú%_Model", UserID: &owner},
		{TraceID: "no-owner", RequestID: "anonymous", RequestedModel: "Shared", ActualProviderModel: "unowned"},
		{TraceID: "missing-owner", RequestID: "orphan", RequestedModel: "other", ActualProviderModel: "shared"},
	}
	for i, call := range calls {
		call.ProviderType = "provider-token"
		call.TrafficClass = "llm"
		call.Status = "completed"
		if i == 1 {
			call.Status = "failed"
		}
		call.CreatedAt = now.Add(time.Duration(i) * time.Second)
		if err := NewModelInvocationRepository(s).Create(call); err != nil {
			t.Fatal(err)
		}
	}
	event := &models.AuditEvent{TraceID: "only-event", EventType: "event-token.failed", Message: "message-token %_ 中文", Severity: "error", TrafficClass: "llm", CreatedAt: now.Add(10 * time.Second)}
	if err := NewAuditEventRepository(s).Create(event); err != nil {
		t.Fatal(err)
	}
	completeAuditBackfill(t, s)
	// A retained report can outlive its user, and must still match other fields.
	execute(`UPDATE observation_audit_trace_reports SET user_id=999991 WHERE trace_id='missing-owner'`)
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	since, until := now.Add(-time.Hour), now.Add(time.Hour)
	check := func(t *testing.T, query AuditReportQuery) []string {
		t.Helper()
		query.Since, query.Until, query.Limit = &since, &until, 2
		normalized, _, err := normalizeAuditReportQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		where := ` FROM observation_audit_trace_reports r LEFT JOIN users u ON u.id=r.user_id WHERE r.id>0 AND r.sort_at>=? AND r.sort_at<?`
		args := []any{since, until}
		if normalized.Status != "" {
			where += ` AND r.status=?`
			args = append(args, normalized.Status)
		}
		if normalized.Model != "" {
			where += ` AND (LOCATE(?,LOWER(r.requested_model))>0 OR LOCATE(?,LOWER(r.actual_provider_model))>0)`
			args = append(args, normalized.Model, normalized.Model)
		}
		if normalized.Search != "" {
			clauses := []string{}
			for _, field := range []string{"r.requested_model", "r.actual_provider_model", "u.username", "r.trace_id", "r.request_id", "r.provider_type", "r.event_type", "r.message"} {
				clauses = append(clauses, `LOCATE(?,LOWER(COALESCE(`+field+`,'')))>0`)
				args = append(args, normalized.Search)
			}
			where += ` AND (` + strings.Join(clauses, ` OR `) + `)`
		}
		rows, err := s.SQL().Query(`SELECT r.trace_id`+where+` ORDER BY r.sort_at DESC,r.trace_id DESC`, args...)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{}
		for rows.Next() {
			var trace string
			if err = rows.Scan(&trace); err != nil {
				t.Fatal(err)
			}
			want = append(want, trace)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		got := []string{}
		for pages := 0; ; pages++ {
			if pages > len(want) {
				t.Fatal("cursor did not finish")
			}
			page, err := r.ListAuditReportPage(ctx, query)
			if err != nil || page.Total != len(want) {
				t.Fatalf("search=%q status=%q model=%q: total=%+v want=%v err=%v", query.Search, query.Status, query.Model, page, want, err)
			}
			for _, item := range page.Items {
				got = append(got, item.TraceID)
			}
			if page.NextCursor == "" {
				break
			}
			query.Cursor = page.NextCursor
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("search=%q: got=%v want=%v", query.Search, got, want)
		}
		return got
	}
	for _, text := range []string{"", "shared", " OWNERTOKEN ", "ñandú", "%", "_", "provider-token", "trace-token", "request-token", "event-token", "message-token", "中文", "not present"} {
		t.Run(text, func(t *testing.T) { check(t, AuditReportQuery{Search: text}) })
	}
	check(t, AuditReportQuery{Search: "provider-token", Status: "failed", Model: "ÑANDÚ"})
	if got := check(t, AuditReportQuery{Search: "shared"}); len(got) != 4 {
		t.Fatalf("overlapping fields or missing owners counted incorrectly: %v", got)
	}
	execute(`UPDATE users SET username='RenamedOwner' WHERE id=9101`)
	if got := check(t, AuditReportQuery{Search: "OwnerToken"}); len(got) != 0 {
		t.Fatalf("stale username retained: %v", got)
	}
	if got := check(t, AuditReportQuery{Search: "RenamedOwner"}); len(got) != 2 {
		t.Fatalf("current username omitted: %v", got)
	}
}
