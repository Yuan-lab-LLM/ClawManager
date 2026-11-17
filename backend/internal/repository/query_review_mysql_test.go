package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"errors"
	"github.com/upper/db/v4"
	"testing"
	"time"
)

func TestQueryReviewCostRebuildBeforeReceipts(t *testing.T) {
	s := reportingFixture(t)
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	_, err := s.SQL().Exec(`INSERT INTO cost_records(trace_id,provider_type,model_name,currency,estimated_cost,prompt_tokens,completion_tokens,total_tokens,recorded_at) VALUES('raw-before-rebuild','openai','m','USD',2,7,3,10,?)`, day)
	if err != nil {
		t.Fatal(err)
	}
	if err = RebuildObservationDay(context.Background(), s, day); err != nil {
		t.Fatal(err)
	}
	seedCostSummaryFixture(t, s)
	until := day.Add(24 * time.Hour)
	report, err := NewCostRecordRepository(s).(*costRecordRepository).CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &day, Until: &until})
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 1 || report.Totals.EstimatedCost != 2 || report.Totals.TotalTokens != 10 {
		t.Fatalf("unreceipted rebuild double counted: count=%d cost=%v tokens=%d", report.Total, report.Totals.EstimatedCost, report.Totals.TotalTokens)
	}
	if err = RebuildObservationDay(context.Background(), s, day); err != nil {
		t.Fatal(err)
	}
	seedCostSummaryFixture(t, s)
	report, err = NewCostRecordRepository(s).(*costRecordRepository).CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &day, Until: &until})
	if err != nil || report.Total != 1 || report.Totals.EstimatedCost != 2 {
		t.Fatalf("backfill/rebuild/replay: %+v %v", report, err)
	}
}

func TestQueryReviewAuditLegacyAfterCompletion(t *testing.T) {
	s := reportingFixture(t)
	completeAuditBackfill(t, s)
	_, err := s.SQL().Exec(`INSERT INTO audit_events(trace_id,event_type,message,severity,traffic_class,created_at) VALUES('late-old-writer','gateway.request.failed','legacy late','error','llm',UTC_TIMESTAMP()-INTERVAL 1 HOUR)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewModelInvocationRepository(s).(*modelInvocationRepository).ListAuditReportPage(context.Background(), AuditReportQuery{Limit: 20}); err == nil {
		t.Fatal("pending legacy audit was falsely reported complete")
	}
	completeAuditBackfill(t, s)
	report, err := NewModelInvocationRepository(s).(*modelInvocationRepository).ListAuditReportPage(context.Background(), AuditReportQuery{Limit: 20, Search: "late-old-writer"})
	if err != nil || report.Total != 1 {
		t.Fatalf("late audit source lost: %+v %v", report, err)
	}
}

func TestQueryReviewMetadataMembershipInvalidatesCursor(t *testing.T) {
	s := reportingFixture(t)
	at := time.Now().UTC().Add(-time.Hour)
	addReportingInvocation(t, s, 1, 9, "a", "a", "m", "completed", at)
	addReportingInvocation(t, s, 2, 9, "b", "b", "m", "completed", at.Add(-time.Minute))
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	first, err := r.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(`INSERT INTO chat_sessions(id,instance_id,session_id,title)VALUES(1,9,'b','renamed')`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{Cursor: first.NextCursor}); !errors.Is(err, ErrSessionCursorStale) {
		t.Fatalf("title/search mutation accepted: %v", err)
	}
	admin, err := r.ListAdminSessionUsageReport(1, 1, "", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec(`UPDATE instances SET name='renamed' WHERE id=10`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ListAdminSessionUsageReport(1, 1, "", SessionUsageFilter{Cursor: admin.NextCursor}); !errors.Is(err, ErrSessionCursorStale) {
		t.Fatalf("admin metadata mutation accepted: %v", err)
	}
}

func TestQueryReviewAuditLegacyCommitBehindCursor(t *testing.T) {
	s := reportingFixture(t)
	driver := s.Driver().(*sql.DB)
	held, err := driver.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	_, err = held.Exec(`INSERT INTO audit_events(trace_id,event_type,message,severity,traffic_class,created_at) VALUES('held-old-writer','gateway.request.failed','held legacy','error','llm',UTC_TIMESTAMP()-INTERVAL 1 HOUR)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SQL().Exec(`INSERT INTO audit_events(trace_id,event_type,message,severity,traffic_class,created_at) VALUES('later-id-committed','gateway.request.failed','newer commit','error','llm',UTC_TIMESTAMP()-INTERVAL 1 HOUR)`)
	if err != nil {
		t.Fatal(err)
	}
	completeAuditBackfill(t, s)
	if err = held.Commit(); err != nil {
		t.Fatal(err)
	}
	completeAuditBackfill(t, s)
	report, err := NewModelInvocationRepository(s).(*modelInvocationRepository).ListAuditReportPage(context.Background(), AuditReportQuery{Limit: 20, Search: "held-old-writer"})
	if err != nil || report.Total != 1 {
		t.Fatalf("lower ID committed behind complete cursor lost: %+v %v", report, err)
	}
}

func TestQueryReviewSessionLateFactsRejectContinuation(t *testing.T) {
	s := reportingFixture(t)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	addReportingInvocation(t, s, 1, 9, "a", "a", "m", "completed", at)
	addReportingInvocation(t, s, 2, 9, "b", "b", "m", "completed", at.Add(-time.Minute))
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	first, err := r.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	addReportingInvocation(t, s, 3, 9, "late-b", "b", "m", "completed", at.Add(time.Minute))
	if next, err := r.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{Cursor: first.NextCursor}); err == nil {
		t.Fatalf("mutable session continuation silently returned %+v", next)
	}
}

func TestQueryReviewAdminLateFactsRejectContinuation(t *testing.T) {
	s := reportingFixture(t)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	addReportingInvocation(t, s, 1, 9, "a", "a", "m", "completed", at)
	addReportingInvocation(t, s, 2, 9, "a2", "a", "m", "completed", at)
	addReportingInvocation(t, s, 3, 10, "b", "b", "m", "completed", at)
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	first, err := r.ListAdminSessionUsageReport(1, 1, "", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	addReportingInvocation(t, s, 4, 10, "late-b", "b", "m", "completed", at)
	addReportingInvocation(t, s, 5, 10, "late-b2", "b", "m", "completed", at)
	if next, err := r.ListAdminSessionUsageReport(1, 1, "", SessionUsageFilter{Cursor: first.NextCursor}); err == nil {
		t.Fatalf("mutable admin continuation silently returned %+v", next)
	}
}

func TestQueryReviewCostRebuildSerializesWithBackfill(t *testing.T) {
	for _, first := range []string{"rebuild", "backfill"} {
		t.Run(first, func(t *testing.T) {
			s := reportingFixture(t)
			ctx := context.Background()
			day := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
			if _, err := s.SQL().Exec(`INSERT INTO cost_records(id,trace_id,provider_type,model_name,currency,estimated_cost,total_tokens,recorded_at)VALUES(1,'concurrent','openai','m','USD',2,10,?)`, day); err != nil {
				t.Fatal(err)
			}
			held := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- s.Tx(func(tx db.Session) error {
					var err error
					if first == "rebuild" {
						err = rebuildObservationDay(ctx, tx, day)
					} else {
						err = materializeCostBatch(ctx, tx, []models.CostRecord{{ID: 1, TraceID: "concurrent", Currency: "USD", EstimatedCost: 2}})
					}
					if err != nil {
						return err
					}
					close(held)
					<-release
					return nil
				})
			}()
			select {
			case <-held:
			case err := <-done:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("first transaction blocked")
			}
			next := make(chan error, 1)
			go func() {
				if first == "rebuild" {
					_, err := ReconcileCostSummaries(ctx, s, 500)
					next <- err
				} else {
					next <- RebuildObservationDay(ctx, s, day)
				}
			}()
			select {
			case err := <-next:
				close(release)
				t.Fatalf("second transaction escaped day fence: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := <-next; err != nil {
				t.Fatal(err)
			}
			seedCostSummaryFixture(t, s)
			until := day.Add(24 * time.Hour)
			report, err := NewCostRecordRepository(s).(*costRecordRepository).CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &day, Until: &until})
			if err != nil || report.Total != 1 || report.Totals.EstimatedCost != 2 || report.Totals.TotalTokens != 10 {
				t.Fatalf("concurrent repair %+v %v", report, err)
			}
		})
	}
}

func TestQueryReviewLowerIDCommitAndColdContinuation(t *testing.T) {
	s := reportingFixture(t)
	at := time.Now().UTC().Truncate(24 * time.Hour).Add(-72 * time.Hour)
	until := at.Add(48 * time.Hour)
	since := at.Add(-24 * time.Hour)
	addReportingInvocation(t, s, 1, 9, "first", "a", "m", "completed", at)
	held := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- s.Tx(func(tx db.Session) error {
			if _, err := tx.SQL().Exec(`INSERT INTO model_invocations(id,trace_id,session_id,request_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,status,total_tokens,created_at)VALUES(2,'lower-held','b','lower',9,'openai','m','m','llm','completed',0,?)`, at.Add(24*time.Hour)); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("held writer blocked")
	}
	addReportingInvocation(t, s, 3, 9, "higher-committed", "b", "m", "completed", at.Add(-time.Minute))
	r := NewModelInvocationRepository(s).(*modelInvocationRepository)
	filter := SessionUsageFilter{Since: &since, Until: &until}
	first, err := r.ListSessionUsageReport(9, 1, 1, "", filter)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	// Archiving hot rows and later, out-of-window traffic do not invalidate facts.
	if _, err = s.SQL().Exec(`DELETE FROM model_invocations WHERE id=1 OR id=3`); err != nil {
		close(release)
		t.Fatal(err)
	}
	addReportingInvocation(t, s, 4, 9, "outside-window", "z", "m", "completed", until.Add(time.Second))
	filter.Cursor = first.NextCursor
	if _, err = r.ListSessionUsageReport(9, 1, 1, "", filter); err != nil {
		close(release)
		t.Fatalf("cold/outside changes invalidated frozen window: %v", err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileObservationSummaries(context.Background(), s, 500); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ListSessionUsageReport(9, 1, 1, "", filter); !errors.Is(err, ErrSessionCursorStale) {
		t.Fatalf("lower-ID zero-token commit not rejected: %v", err)
	}
}

func TestQueryReviewCreateFencesBeforeSourceInsert(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	held := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	writer := make(chan error, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	go func() {
		first <- s.Tx(func(tx db.Session) error {
			if err := lockSummaryDays(ctx, tx, true, at); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-first:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("rebuild lock blocked")
	}
	go func() {
		writer <- NewCostRecordRepository(s).Create(&models.CostRecord{TraceID: "", ProviderType: "openai", ModelName: "m", Currency: "USD", EstimatedCost: 2, RecordedAt: at})
	}()
	select {
	case err := <-writer:
		t.Fatalf("source escaped rebuild fence: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	probe, err := s.Driver().(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var id int
	err = probe.QueryRowContext(ctx, `SELECT id FROM cost_records WHERE id=1 FOR UPDATE NOWAIT`).Scan(&id)
	probe.Rollback()
	if err != sql.ErrNoRows {
		t.Fatalf("writer inserted raw source before taking day fence: %v", err)
	}
	close(release)
	if err = <-first; err != nil {
		t.Fatal(err)
	}
	if err = <-writer; err != nil {
		t.Fatal(err)
	}
}
