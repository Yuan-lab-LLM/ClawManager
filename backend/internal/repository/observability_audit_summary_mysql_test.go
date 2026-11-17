package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func auditFullFixture(t *testing.T) db.Session {
	t.Helper()
	seed := databaseReportingMySQL(t)
	row, err := seed.SQL().QueryRow("SELECT DATABASE()")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = row.Scan(&name); err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(os.Getenv("TEST_DATABASE_MYSQL_PORT"))
	s, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Password: os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), Database: name, MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAuditSummaryCursorFreezeBindingsAndRange(t *testing.T) {
	s := reportingFixture(t)
	ctx := context.Background()
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	now := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i, trace := range []string{"trace-a", "trace-b", "trace-c"} {
		addReportingInvocation(t, s, i+1, 9, trace, "main", "model", "completed", now)
	}
	// A fee can reserve a trace lock before any invocation/event materializes it.
	if err := s.Tx(func(tx db.Session) error { return lockAuditTraces(ctx, tx, []string{"reserved-before-page"}) }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 2}); err == nil {
		t.Fatal("partial bootstrap report was served")
	} else {
		var pending *SummaryRebuildingError
		if !errors.As(err, &pending) {
			t.Fatal(err)
		}
	}
	completeAuditBackfill(t, s)
	first, err := r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].TraceID != "trace-c" || first.Items[1].TraceID != "trace-b" || first.Total != 3 || first.NextCursor == "" || first.Until.Sub(first.Since) != 30*24*time.Hour {
		t.Fatalf("first %+v", first)
	}
	addReportingInvocation(t, s, 10, 9, "trace-new-backdated", "main", "model", "completed", now.Add(-time.Minute))
	addReportingInvocation(t, s, 11, 9, "trace-b", "main", "model", "completed", now.Add(-2*time.Hour))
	addReportingInvocation(t, s, 12, 9, "reserved-before-page", "main", "model", "completed", now.Add(-time.Minute))
	next, err := r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if next.Total != 3 || len(next.Items) != 1 || next.Items[0].TraceID != "trace-a" || next.NextCursor != "" || !next.Until.Equal(first.Until) || !next.Since.Equal(first.Since) {
		t.Fatalf("cursor moved or duplicated: %+v", next)
	}
	until := first.Until.Add(time.Minute)
	for _, q := range []AuditReportQuery{{Limit: 2, Cursor: first.NextCursor, Search: "different"}, {Limit: 2, Cursor: first.NextCursor, Status: "failed"}, {Limit: 2, Cursor: first.NextCursor, Model: "other"}, {Limit: 1, Cursor: first.NextCursor}, {Limit: 2, Cursor: first.NextCursor, Until: &until}, {Limit: 2, Cursor: "garbage"}, {Limit: 101}} {
		if _, err = r.ListAuditReportPage(ctx, q); !errors.Is(err, ErrInvalidAuditQuery) {
			t.Fatalf("bad query accepted %+v: %v", q, err)
		}
	}
	start := now.Add(-91 * 24 * time.Hour)
	if _, err = r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 2, Since: &start, Until: &now}); !errors.Is(err, ErrInvalidAuditQuery) {
		t.Fatalf("91 days accepted: %v", err)
	}
	start = now.Add(-90 * 24 * time.Hour)
	if _, err = r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 2, Since: &start, Until: &now}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditSummaryLegacyHooksColdEventsReplayAndMixedSources(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	event := &models.AuditEvent{TraceID: "event-only", EventType: "gateway.request.failed", Message: "specific failure", Severity: "error", TrafficClass: "llm", CreatedAt: now}
	if err := NewAuditEventRepository(s).Create(event); err != nil {
		t.Fatal(err)
	}
	fee := &models.CostRecord{TraceID: event.TraceID, ProviderType: "openai", ModelName: "fallback-model", Currency: "USD", PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10, RecordedAt: now}
	if err := NewCostRecordRepository(s).Create(fee); err != nil {
		t.Fatal(err)
	}
	call := &models.ModelInvocation{TraceID: "mixed", RequestID: "earliest-request", RequestedModel: "auto", ActualProviderModel: "older-model", ProviderType: "openai", TrafficClass: "llm", Status: "failed", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CreatedAt: now.Add(-time.Hour)}
	if err := NewModelInvocationRepository(s).Create(call); err != nil {
		t.Fatal(err)
	}
	// A legacy hot row has no narrow summary yet. The same trace must combine it
	// with the existing summary before the legacy row is backfilled.
	_, err := s.SQL().Exec(`INSERT INTO model_invocations(trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,prompt_tokens,completion_tokens,total_tokens,created_at) VALUES('mixed','latest-request','auto','latest-model','openai','llm','completed',20,10,30,?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	// Already verified archive rows must still be included by the new audit cutover.
	_, err = s.SQL().Exec(`INSERT INTO history_archive_records(kind,original_id,owner_id,instance_id,trace_id,session_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at,summary_verified_at,summary_sha256) VALUES('audit_events',900,1,0,'already-cold','',?,'month','{}',REPEAT('a',64),10,'{"event_type":"gateway.request.blocked","message":"cold-only reason"}',?,?,?,REPEAT('a',64))`, now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	completeAuditBackfill(t, s)
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	p, err := r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 20, Search: "mixed"})
	if err != nil || p.Total != 1 || p.Items[0].TotalTokens != 45 || p.Items[0].RequestID != "earliest-request" || p.Items[0].Status != "completed" || p.Items[0].ActualProviderModel != "latest-model" {
		t.Fatalf("mixed partial source lost: %+v %v", p, err)
	}
	p, err = r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 20, Search: "specific failure", Status: "failed", Model: "fallback-model"})
	if err != nil || p.Total != 1 || p.Items[0].TotalTokens != 10 {
		t.Fatalf("event fee hook %+v %v", p, err)
	}
	if _, err = s.SQL().Exec("DELETE FROM audit_events"); err != nil {
		t.Fatal(err)
	}
	if err = s.Tx(func(tx db.Session) error { return maintainObservationSummary(ctx, tx, "audit_events", event) }); err != nil {
		t.Fatal(err)
	}
	p, err = r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 20, Search: "specific failure"})
	if err != nil || p.Total != 1 || p.Items[0].TotalTokens != 10 {
		t.Fatalf("archived event replay: %+v %v", p, err)
	}
	p, err = r.ListAuditReportPage(ctx, AuditReportQuery{Limit: 20, Search: "cold-only reason", Status: "blocked"})
	if err != nil || p.Total != 1 || p.Items[0].TraceID != "already-cold" {
		t.Fatalf("preexisting archive skipped: %+v %v", p, err)
	}
}

func TestAuditSummaryRollbackAndConcurrentTraceWriters(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	expected := errors.New("rollback")
	err := s.Tx(func(tx db.Session) error {
		v := &models.AuditEvent{ID: 1, TraceID: "rolled-back", CreatedAt: now}
		if err := maintainObservationSummary(ctx, tx, "audit_events", v); err != nil {
			return err
		}
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatal(err)
	}
	row, _ := s.SQL().QueryRow("SELECT COUNT(*) FROM observation_audit_trace_reports WHERE trace_id='rolled-back'")
	var n int
	if err = row.Scan(&n); err != nil || n != 0 {
		t.Fatalf("rolled back summary retained: %d %v", n, err)
	}
	completeAuditBackfill(t, s)
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			results <- NewModelInvocationRepository(s).Create(&models.ModelInvocation{TraceID: "concurrent", RequestID: fmt.Sprint(i), RequestedModel: "auto", ActualProviderModel: "m", ProviderType: "openai", TrafficClass: "llm", Status: "completed", TotalTokens: 5, CreatedAt: now})
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err = <-results; err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewModelInvocationRepository(s).(*modelInvocationRepository).ListAuditReportPage(ctx, AuditReportQuery{Limit: 20})
	if err != nil || p.Total != 1 || p.Items[0].TotalTokens != 40 {
		t.Fatalf("lost concurrent contributions: %+v %v", p, err)
	}
}

func TestAuditErrorPreviewKeepsCompleteStoredDetail(t *testing.T) {
	s := auditFullFixture(t)
	message := strings.Repeat("error-details ", 200)
	call := &models.ModelInvocation{TraceID: "long-error", RequestID: "error", RequestedModel: "auto", ActualProviderModel: "model", ProviderType: "openai", TrafficClass: "llm", Status: "failed", ErrorMessage: &message, CreatedAt: time.Now().UTC().Add(-time.Minute)}
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	if err := r.Create(call); err != nil {
		t.Fatal(err)
	}
	completeAuditBackfill(t, s)
	p, err := r.ListAuditReportPage(context.Background(), AuditReportQuery{Limit: 20})
	if err != nil || len(p.Items) != 1 || p.Items[0].ErrorMessage == nil || len(*p.Items[0].ErrorMessage) != 1000 || !p.TruncatedErrors[call.ID] {
		t.Fatalf("explicit error preview: %+v %v", p, err)
	}
	full, err := r.GetByID(call.ID)
	if err != nil || full.ErrorMessage == nil || *full.ErrorMessage != message {
		t.Fatalf("full detail lost: %+v %v", full, err)
	}
	row, _ := s.SQL().QueryRow("SELECT error_message FROM observation_invocation_summaries WHERE id=?", call.ID)
	var persisted string
	if err = row.Scan(&persisted); err != nil || persisted != message {
		t.Fatalf("summary changed original error %v", err)
	}
}
