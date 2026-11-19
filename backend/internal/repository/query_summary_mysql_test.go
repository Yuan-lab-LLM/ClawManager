package repository

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"github.com/upper/db/v4"
	"os"
	"strings"
	"testing"
	"time"
)

func querySummaryFixture(t *testing.T) db.Session {
	t.Helper()
	s := reportingFixture(t)
	for _, file := range []string{"114_add_history_maintenance.sql", "116_add_query_summaries.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				lines = append(lines, line)
			}
		}
		for _, q := range strings.Split(strings.Join(lines, "\n"), ";") {
			if strings.TrimSpace(q) != "" {
				if _, err = s.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return s
}
func TestQuerySummaryDedupAndArchiveProof(t *testing.T) {
	s := querySummaryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	owner, instance := 1, 9
	session := "main"
	uid := "stable-event"
	v := &models.ModelInvocation{ID: 1, UserID: &owner, InstanceID: &instance, SessionID: &session, EventUID: &uid, TraceID: "trace", RequestID: "req", Status: "completed", TotalTokens: 15, PromptTokens: 10, CompletionTokens: 5, CreatedAt: now}
	for i := 0; i < 2; i++ {
		if err := s.Tx(func(tx db.Session) error { return maintainObservationSummary(ctx, tx, "model_invocations", v) }); err != nil {
			t.Fatal(err)
		}
	}
	var n, tokens int
	row, _ := s.SQL().QueryRow("SELECT COUNT(*),SUM(total_tokens) FROM observation_daily_summaries WHERE scope='session'")
	if err := row.Scan(&n, &tokens); err != nil || n != 1 || tokens != 15 {
		t.Fatalf("dedup: %d %d %v", n, tokens, err)
	}
	_, err := s.SQL().Exec(`INSERT INTO history_archive_records(kind,original_id,owner_id,instance_id,trace_id,session_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at) VALUES('model_invocations',1,1,9,'trace','main',?,'month','{}',REPEAT('a',64),10,'{"request_id":"req","status":"completed","prompt_tokens":10,"completion_tokens":5,"total_tokens":15}',?,?)`, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileObservationSummaries(ctx, s, 500); err != nil {
		t.Fatal(err)
	}
	var proof int
	row, _ = s.SQL().QueryRow("SELECT COUNT(*) FROM history_archive_records WHERE summary_verified_at IS NOT NULL AND summary_sha256=payload_sha256")
	row.Scan(&proof)
	if proof != 1 {
		t.Fatal("archive proof missing")
	}
	row, _ = s.SQL().QueryRow("SELECT SUM(total_tokens) FROM observation_daily_summaries WHERE scope='session'")
	row.Scan(&tokens)
	if tokens != 15 {
		t.Fatalf("hot+cold duplicate: %d", tokens)
	}
}

func TestTraceCursorPagesBoundedAndStable(t *testing.T) {
	s := querySummaryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i := 1; i <= 205; i++ {
		addReportingInvocation(t, s, i, 9, "paged", "main", "m", "completed", now)
	}
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	first, err := repo.TracePage(context.Background(), "paged", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 200 || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	addReportingInvocation(t, s, 300, 9, "paged", "main", "m", "completed", now.Add(-time.Hour))
	second, err := repo.TracePage(context.Background(), "paged", first.NextCursor, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 5 || second.NextCursor != "" {
		t.Fatalf("snapshot continuation: %+v", second)
	}
	if _, err = repo.TracePage(context.Background(), "other", first.NextCursor, 200); err == nil {
		t.Fatal("cross trace cursor accepted")
	}
}

func TestQuerySummaryColdCostRebuildAndCursor(t *testing.T) {
	s := querySummaryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	addReportingInvocation(t, s, 1, 9, "retry", "a", "m", "failed", now)
	addReportingInvocation(t, s, 2, 9, "retry", "a", "m", "completed", now)
	addReportingInvocation(t, s, 3, 9, "b", "b", "m", "completed", now)
	for _, q := range []string{`INSERT INTO cost_records(trace_id,session_id,instance_id,invocation_id,provider_type,model_name,estimated_cost) VALUES('retry','a',9,1,'openai','m',1),('retry','a',9,2,'openai','m',2),('retry','a',9,NULL,'openai','m',3)`, `DELETE FROM model_invocations`, `UPDATE observation_daily_summaries SET total_tokens=999 WHERE scope='session'`} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e := RebuildObservationDay(context.Background(), s, now); e != nil {
		t.Fatal(e)
	}
	seedCostSummaryFixture(t, s)
	first, e := repo.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{})
	if e != nil {
		t.Fatal(e)
	}
	if first.NextCursor == "" || first.Summary.TotalTokens != 45 || first.Summary.EstimatedCost != 6 {
		t.Fatalf("cold summary: %+v", first)
	}
	second, e := repo.ListSessionUsageReport(9, 1, 1, "", SessionUsageFilter{Cursor: first.NextCursor})
	if e != nil || len(second.Items) != 1 || second.Items[0].SessionID == first.Items[0].SessionID || second.NextCursor != "" {
		t.Fatalf("cursor: %+v %v", second, e)
	}
	costs, e := NewCostRecordRepository(s).(*costRecordRepository).AggregateModelCostByInstanceSession(9, "a", SessionUsageFilter{})
	if e != nil || len(costs) != 1 || costs[0].EstimatedCost != 6 {
		t.Fatalf("cold fees duplicated/lost: %+v %v", costs, e)
	}
}

func TestTraceBodyChunksPreserveContentAndScope(t *testing.T) {
	s := querySummaryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	addReportingInvocation(t, s, 1, 9, "body", "main", "m", "completed", now)
	body := strings.Repeat("界", 400000)
	if _, e := s.SQL().Exec(`UPDATE model_invocations SET request_payload=? WHERE id=1`, body); e != nil {
		t.Fatal(e)
	}
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	page, e := repo.TracePage(context.Background(), "body", "", 200)
	if e != nil || len(page.Records) != 1 {
		t.Fatalf("metadata: %+v %v", page, e)
	}
	if strings.Contains(string(page.Records[0].Data), "request_payload") {
		t.Fatal("page eagerly contains body")
	}
	var invocation models.ModelInvocation
	if e = json.Unmarshal(page.Records[0].Data, &invocation); e != nil {
		t.Fatalf("metadata JSON incompatible: %v", e)
	}
	text := ""
	offset := 0
	chunks := 0
	for {
		chunk, err := repo.TraceBody(context.Background(), "body", "model_invocations", 1, offset)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(chunk)
		if len(encoded) > TracePageBytes {
			t.Fatal("chunk budget exceeded")
		}
		text += chunk.Data
		chunks++
		offset = chunk.NextOffset
		if offset == 0 {
			break
		}
	}
	if chunks < 2 {
		t.Fatal("large body did not paginate")
	}
	if e = json.Unmarshal([]byte(text), &invocation); e != nil || invocation.RequestPayload == nil || *invocation.RequestPayload != body {
		t.Fatal("body chunks lost content", e)
	}
	if _, e = repo.TraceBody(context.Background(), "other", "model_invocations", 1, 0); e == nil {
		t.Fatal("cross trace body returned")
	}
}

func TestQuerySummaryBackfillReadinessAndTimedReconciliation(t *testing.T) {
	s := querySummaryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	addReportingInvocation(t, s, 1, 9, "backfill", "main", "m", "completed", now)
	if _, e := s.SQL().Exec(`UPDATE observation_summary_state SET target_id=1,ready=FALSE WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	if _, e := repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{}); e == nil {
		t.Fatal("partial backfill silently served")
	}
	if _, e := ReconcileObservationSummaries(context.Background(), s, 500); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SQL().Exec(`UPDATE observation_daily_summaries SET total_tokens=999 WHERE scope='session'`); e != nil {
		t.Fatal(e)
	}
	if e := ReconcileObservationDay(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	report, e := repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{})
	if e != nil || report.Summary.TotalTokens != 15 {
		t.Fatalf("worker reconciliation: %+v %v", report, e)
	}
}

func seedCostSummaryFixture(t *testing.T, s db.Session) {
	t.Helper()
	for {
		n, e := ReconcileCostSummaries(context.Background(), s, 500)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
	}
}

func TestCostSummaryOutOfOrderDedupAndWindow(t *testing.T) {
	s := querySummaryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	instance := 9
	session := "main"
	invocation := 41
	fee := &models.CostRecord{ID: 1, TraceID: "late", InvocationID: &invocation, InstanceID: &instance, SessionID: &session, EstimatedCost: 2, Currency: "USD"}
	for i := 0; i < 2; i++ {
		if e := s.Tx(func(tx db.Session) error { return maintainCostSummary(ctx, tx, fee) }); e != nil {
			t.Fatal(e)
		}
	}
	var ready bool
	row, _ := s.SQL().QueryRow(`SELECT ready FROM observation_summary_state WHERE id=2`)
	row.Scan(&ready)
	if ready {
		t.Fatal("unknown dependency reported ready")
	}
	addReportingInvocation(t, s, invocation, 9, "late", "main", "m", "completed", now)
	for i := 0; i < 2; i++ {
		if e := s.Tx(func(tx db.Session) error { return maintainCostSummary(ctx, tx, fee) }); e != nil {
			t.Fatal(e)
		}
	}
	legacy := &models.CostRecord{ID: 2, TraceID: "late", InstanceID: &instance, SessionID: &session, EstimatedCost: 3, Currency: "USD"}
	for i := 0; i < 2; i++ {
		if e := s.Tx(func(tx db.Session) error { return maintainCostSummary(ctx, tx, legacy) }); e != nil {
			t.Fatal(e)
		}
	}
	addReportingInvocation(t, s, 42, 9, "late", "main", "m", "failed", now.Add(-24*time.Hour))
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	since := now.Add(-time.Hour)
	until := now.Add(time.Hour)
	result, e := repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{Since: &since, Until: &until})
	if e != nil || result.Summary.EstimatedCost != 5 {
		t.Fatalf("late/dedup/window: %+v %v", result, e)
	}
	before := now.Add(-48 * time.Hour)
	past := now.Add(-2 * time.Hour)
	result, e = repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{Since: &before, Until: &past})
	if e != nil || result.Summary.EstimatedCost != 3 {
		t.Fatalf("legacy once, exact outside window: %+v %v", result, e)
	}
}

func TestCostSummaryRealCreateBeforeInvocation(t *testing.T) {
	s := auditFullFixture(t)
	for _, q := range []string{
		`INSERT IGNORE INTO users(id,username,email,password_hash)VALUES(1,'fees','fees@example.test','fixture')`,
		`INSERT INTO instances(id,user_id,name,type,status,cpu_cores,memory_gb,disk_gb,os_type,os_version)VALUES(9,1,'fee-test','openclaw','running',1,1,1,'linux','fixture')`,
	} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	instance, invocation := 9, 1
	session := "main"
	fee := &models.CostRecord{TraceID: "", InstanceID: &instance, SessionID: &session, InvocationID: &invocation, ProviderType: "openai", ModelName: "m", Currency: "USD", EstimatedCost: 2}
	costs := NewCostRecordRepository(s)
	if e := costs.Create(fee); e != nil {
		t.Fatal(e)
	}
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	if _, e := repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{}); e == nil {
		t.Fatal("unresolved real ledger dependency silently served")
	}
	call := &models.ModelInvocation{TraceID: "", RequestID: "late-real", InstanceID: &instance, SessionID: &session, RequestedModel: "m", ActualProviderModel: "m", ProviderType: "openai", TrafficClass: "llm", Status: "completed", TotalTokens: 15}
	if e := repo.Create(call); e != nil {
		t.Fatal(e)
	}
	if call.ID != invocation {
		t.Fatalf("fixture ID %d", call.ID)
	}
	report, e := repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{})
	if e != nil || report.Summary.EstimatedCost != 2 {
		t.Fatalf("real source dependency unresolved: %+v %v", report, e)
	}
	for i := 0; i < 2; i++ {
		if _, e = ReconcileObservationSummaries(context.Background(), s, 500); e != nil {
			t.Fatal(e)
		}
		if _, e = ReconcileCostSummaries(context.Background(), s, 500); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.SQL().Exec(`DELETE FROM model_invocations`); e != nil {
		t.Fatal(e)
	}
	report, e = repo.ListSessionUsageReport(9, 1, 20, "", SessionUsageFilter{})
	if e != nil || report.Summary.EstimatedCost != 2 || report.Summary.TotalTokens != 15 {
		t.Fatalf("replay/cold changed fees: %+v %v", report, e)
	}
}

func TestQuerySummaryArchiveNullScope(t *testing.T) {
	s := querySummaryFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	call := &models.ModelInvocation{ID: 5, TraceID: "unattributed", RequestID: "scope", Status: "completed", CreatedAt: now}
	if e := s.Tx(func(tx db.Session) error {
		return maintainObservationSummary(context.Background(), tx, "model_invocations", call)
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SQL().Exec(`INSERT INTO history_archive_records(kind,original_id,owner_id,instance_id,trace_id,session_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at)VALUES('model_invocations',5,0,0,'unattributed','',?,'month','{}',REPEAT('a',64),10,'{"request_id":"scope","status":"completed"}',?,?)`, now, now, now); e != nil {
		t.Fatal(e)
	}
	if _, e := ReconcileObservationSummaries(context.Background(), s, 500); e != nil {
		t.Fatal(e)
	}
	row, _ := s.SQL().QueryRow(`SELECT COUNT(*) FROM history_archive_records WHERE summary_verified_at IS NOT NULL AND summary_sha256=payload_sha256`)
	var n int
	if e := row.Scan(&n); e != nil || n != 1 {
		t.Fatal("null-scope archive proof", n, e)
	}
}
