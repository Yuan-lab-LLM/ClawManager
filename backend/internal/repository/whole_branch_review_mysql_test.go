package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"clawreef/internal/observability"
	"github.com/upper/db/v4"
)

type wholeBranchArchive struct {
	objects *observability.EncryptedObjects
}

func (s wholeBranchArchive) Put(ctx context.Context, owner, key string, raw []byte) ([]byte, error) {
	ref, err := s.objects.Put(ctx, owner, raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ref)
}
func (s wholeBranchArchive) Read(ctx context.Context, owner string, raw []byte) ([]byte, error) {
	var ref observability.BodyRef
	if err := json.Unmarshal(raw, &ref); err != nil {
		return nil, err
	}
	return s.objects.Get(ctx, owner, ref)
}

func TestWholeBranchActualArchiveTypedMetadata(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	pool := s.Driver().(*sql.DB)
	objects, err := observability.NewEncryptedObjects(observationObjects{}, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	history := historymaintenance.New(pool, wholeBranchArchive{objects}, nil)
	ConfigureHistoryReader(s, history)
	defer ConfigureHistoryReader(s, nil)
	at := time.Now().UTC().AddDate(0, 0, -100).Truncate(time.Second)
	until := at.Add(time.Hour)
	longError := strings.Repeat("preserved error ", 100)
	for id := 1; id <= 502; id++ {
		var throughput any
		if id == 501 {
			throughput = "12.3456"
		}
		if _, err = pool.ExecContext(ctx, `INSERT INTO model_invocations(id,user_id,trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,is_streaming,tokens_per_second,error_message,created_at,completed_at)VALUES(?,1,?,?,'auto','model','openai','llm','completed',3,?,?,?,?,?)`, id, fmt.Sprintf("archive-%d", id), fmt.Sprintf("archive-%d", id), id%2, throughput, longError, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.ExecContext(ctx, `INSERT INTO audit_events(id,user_id,trace_id,event_type,severity,traffic_class,message,created_at)VALUES(701,1,'audit-only-archive','gateway.request.failed','error','llm','audit message must survive',?)`, at); err != nil {
		t.Fatal(err)
	}
	if _, n, err := history.Backfill(ctx, "model_invocations", at, until, 500, 500); err != nil || n != 2 {
		t.Fatalf("actual backfill %d %v", n, err)
	}
	if _, n, err := history.Backfill(ctx, "audit_events", at, until, 0, 500); err != nil || n != 1 {
		t.Fatalf("audit backfill %d %v", n, err)
	}
	for _, id := range []int{501, 502} {
		var raw []byte
		if err = pool.QueryRowContext(ctx, `SELECT metadata_json FROM history_archive_records WHERE kind='model_invocations' AND original_id=?`, id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var v models.ModelInvocation
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Errorf("SQL-produced metadata id %d not typed: %v; %s", id, err, raw)
			continue
		}
		if v.IsStreaming != (id == 501) || v.ErrorMessage == nil || *v.ErrorMessage != longError || (id == 501 && (v.TokensPerSecond == nil || *v.TokensPerSecond != 12.3456)) || (id == 502 && v.TokensPerSecond != nil) {
			t.Errorf("SQL-produced metadata lost values id %d: %+v", id, v)
		}
	}
	if t.Failed() {
		return
	}
	// First hot batch ends at 500; the archive branch materializes 501/502 first.
	if _, err = ReconcileObservationSummaries(ctx, s, 500); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{501, 502} {
		var got string
		if err = pool.QueryRowContext(ctx, `SELECT error_message FROM observation_invocation_summaries WHERE id=?`, id).Scan(&got); err != nil || got != longError {
			t.Fatalf("archive-first error lost %q %v", got, err)
		}
		record, err := history.Get(ctx, "model_invocations", int64(id), 1, false)
		if err != nil {
			t.Fatal(err)
		}
		var v models.ModelInvocation
		if err = json.Unmarshal(record.Payload, &v); err != nil {
			t.Fatal(err)
		}
	}
	var message string
	if err = pool.QueryRowContext(ctx, `SELECT message FROM observation_audit_event_summaries WHERE id=701`).Scan(&message); err != nil || message != "audit message must survive" {
		t.Fatalf("archive audit message lost %q %v", message, err)
	}
	if _, err = pool.Exec(`UPDATE observation_invocation_summaries SET error_message='truncated stale report',tokens_per_second=NULL WHERE id=501`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(`UPDATE history_archive_records SET summary_verified_at=NULL,summary_sha256='' WHERE kind='model_invocations' AND original_id=501`); err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileObservationSummaries(ctx, s, 500); err == nil {
		t.Fatal("incomplete existing report granted archive deletion proof")
	}
	var proven int
	if err = pool.QueryRow(`SELECT COUNT(*) FROM history_archive_records WHERE kind='model_invocations' AND original_id=501 AND summary_verified_at IS NOT NULL`).Scan(&proven); err != nil || proven != 0 {
		t.Fatalf("bad report proof %d %v", proven, err)
	}
}

// AUTO_INCREMENT orders allocation, not commit. Both sources must be caught
// after the first sweep has already passed a still-uncommitted lower ID.
func TestWholeBranchLegacyCommitBehindInvocationAndCostWatermarks(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	held, err := s.Driver().(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	invocation := `INSERT INTO model_invocations(trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,created_at)VALUES(?,?,'auto','model','openai','llm','completed',0,UTC_TIMESTAMP()-INTERVAL 1 HOUR)`
	cost := `INSERT INTO cost_records(trace_id,provider_type,model_name,currency,estimated_cost,total_tokens,recorded_at)VALUES(?,'openai','model','USD',2,0,UTC_TIMESTAMP()-INTERVAL 1 HOUR)`
	if _, err = held.ExecContext(ctx, invocation, "held-low", "held-low"); err != nil {
		t.Fatal(err)
	}
	if _, err = held.ExecContext(ctx, cost, "held-low"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().ExecContext(ctx, invocation, "committed-high", "committed-high"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().ExecContext(ctx, cost, "committed-high"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = ReconcileObservationSummaries(ctx, s, 500); err != nil {
			t.Fatal(err)
		}
		if _, err = ReconcileCostSummaries(ctx, s, 500); err != nil {
			t.Fatal(err)
		}
	}
	if err = held.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = NewModelInvocationRepository(s).(*modelInvocationRepository).ListAdminSessionUsageReport(1, 20, "", SessionUsageFilter{}); err == nil {
		t.Fatal("late commit was reported summary-ready before queue catchup")
	}
	if _, err = NewCostRecordRepository(s).(*costRecordRepository).CostOverviewReport(1, 20, "", SessionUsageFilter{}); err == nil {
		t.Fatal("late cost commit was reported ready before queue catchup")
	}
	for range 2 {
		if _, err = ReconcileObservationSummaries(ctx, s, 500); err != nil {
			t.Fatal(err)
		}
		if _, err = ReconcileCostSummaries(ctx, s, 500); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"observation_invocation_summaries", "observation_cost_facts"} {
		row, err := s.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE trace_id='held-low'")
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err = row.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("late lower-ID legacy commit omitted from %s: %d", table, n)
		}
	}
}

func TestWholeBranchRegisteredMonthReceivesNewInvocation(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	pool := s.Driver().(*sql.DB)
	history := historymaintenance.New(pool, nil, nil)
	now := time.Now().UTC().Truncate(time.Second)
	if err := history.PrepareMonths(ctx, now); err != nil {
		t.Fatal(err)
	}
	owner := 1
	v := &models.ModelInvocation{UserID: &owner, TraceID: "monthly-new", RequestID: "monthly-new", ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: now, CompletedAt: &now, IsStreaming: true}
	if err := NewModelInvocationRepository(s).Create(v); err != nil {
		t.Fatal(err)
	}
	month, _ := historymaintenance.MonthlyTable("model_invocations", now)
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+month+"` WHERE original_id=?", v.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("registered month never received actual new log write: %s id %d count %d", month, v.ID, count)
	}
}

func TestWholeBranchMonthlyServingCutoverRollbackAndLateLegacy(t *testing.T) {
	s := auditFullFixture(t)
	ctx := context.Background()
	pool := s.Driver().(*sql.DB)
	history := historymaintenance.New(pool, nil, nil)
	ConfigureHistoryReader(s, history)
	defer ConfigureHistoryReader(s, nil)
	now := time.Now().UTC().Truncate(time.Second)
	previous := now.AddDate(0, -1, 0)
	if err := history.PrepareMonths(ctx, previous); err != nil {
		t.Fatal(err)
	}
	owner := 1
	body := "monthly authoritative body"
	session := "monthly-session"
	repo := NewModelInvocationRepository(s).(*modelInvocationRepository)
	var invocations []*models.ModelInvocation
	for _, at := range []time.Time{previous, now} {
		v := &models.ModelInvocation{UserID: &owner, SessionID: &session, TraceID: "monthly-cross", RequestID: fmt.Sprintf("monthly-%d", at.Unix()), ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: at, CompletedAt: &at, IsStreaming: true, TotalTokens: 3, RequestPayload: &body}
		if err := repo.Create(v); err != nil {
			t.Fatal(err)
		}
		invocations = append(invocations, v)
		month, _ := historymaintenance.MonthlyTable("model_invocations", at)
		if _, complete, err := history.VerifyLogMonth(ctx, month, 0); err != nil || !complete {
			t.Fatalf("verify %s %v %v", month, complete, err)
		}
		if err := history.CutoverLogMonth(ctx, month); err != nil {
			t.Fatal(err)
		}
	}
	// Poison only the fixed rollback copies: every active serving path must keep
	// the monthly content, proving this is a real SQL route, not an unused mirror.
	if _, err := pool.Exec(`UPDATE model_invocations SET request_payload='fixed copy',total_tokens=999 WHERE trace_id='monthly-cross'`); err != nil {
		t.Fatal(err)
	}
	for _, v := range invocations {
		got, err := repo.GetByID(v.ID)
		if err != nil || got == nil || got.TotalTokens != 3 || got.RequestPayload == nil || *got.RequestPayload != body {
			t.Fatalf("Get bypassed month: %+v %v", got, err)
		}
	}
	list, err := repo.ListByTraceID("monthly-cross")
	if err != nil || len(list) != 2 || list[0].TotalTokens != 3 {
		t.Fatalf("trace list bypassed month: %+v %v", list, err)
	}
	query := historymaintenance.Query{Kind: "model_invocations", OwnerID: int64(owner), Since: previous.Add(-time.Second), Until: now.Add(time.Second), Limit: 1}
	first, err := history.ListPage(ctx, query)
	if err != nil || len(first.Records) != 1 || !first.HasMore {
		t.Fatalf("crossmonth first %+v %v", first, err)
	}
	query.AfterID = first.NextID
	second, err := history.ListPage(ctx, query)
	if err != nil || len(second.Records) != 1 || second.HasMore || second.Records[0].ID == first.Records[0].ID {
		t.Fatalf("crossmonth second %+v %v", second, err)
	}
	page, err := repo.TracePage(ctx, "monthly-cross", "", 1)
	if err != nil || len(page.Records) != 1 || page.NextCursor == "" {
		t.Fatalf("trace page %+v %v", page, err)
	}
	var payload models.ModelInvocation
	if err = json.Unmarshal(page.Records[0].Data, &payload); err != nil || payload.TotalTokens != 3 {
		t.Fatalf("Trace bypassed month %+v %v", payload, err)
	}
	if _, _, err = history.MonthlyGet(ctx, "model_invocations", int64(invocations[0].ID), 2, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("month owner isolation: %v", err)
	}
	// An old writer can commit after cutover. Until its transaction's queue is
	// drained, active requests fail visibly; it is never hidden by old-copy reads.
	if _, err = pool.Exec(`INSERT INTO model_invocations(user_id,trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,created_at)VALUES(1,'monthly-late','monthly-late','m','m','openai','llm','completed',0,?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetByID(invocations[1].ID); !errors.Is(err, historymaintenance.ErrMonthlyRouteIncomplete) {
		t.Fatalf("legacy catchup silently bypassed: %v", err)
	}
	if _, err = pool.Exec(`UPDATE model_invocations SET request_payload=?,total_tokens=3 WHERE trace_id='monthly-cross'`, body); err != nil {
		t.Fatal(err)
	}
	if _, err = ReconcileObservationSummaries(ctx, s, 500); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetByID(invocations[1].ID); err != nil {
		t.Fatal(err)
	}
	month, _ := historymaintenance.MonthlyTable("model_invocations", now)
	if _, err = pool.Exec("UPDATE `"+month+"` SET payload_json=JSON_SET(payload_json,'$.total_tokens',999) WHERE original_id=?", invocations[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetByID(invocations[1].ID); err == nil {
		t.Fatal("corrupt active payload fell back to valid fixed copy")
	}
	if err = history.RollbackMonth(ctx, month); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(invocations[1].ID)
	if err != nil || got.TotalTokens != 3 {
		t.Fatalf("explicit rollback failed %+v %v", got, err)
	}
	if _, _, err = history.BackfillLogs(ctx, "model_invocations", now.Add(-time.Second), now.Add(time.Second), 0, 500); err != nil {
		t.Fatal(err)
	}
	if _, complete, err := history.VerifyLogMonth(ctx, month, 0); err != nil || !complete {
		t.Fatalf("repair verify %v %v", complete, err)
	}
	if err = history.CutoverLogMonth(ctx, month); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = pool.QueryRow(`SELECT state FROM history_month_catalog WHERE table_name=?`, month).Scan(&state); err != nil || state != "prepared" {
		t.Fatalf("explicit repaired route kept rollback retention state: %s %v", state, err)
	}
	if got, err = repo.GetByID(invocations[1].ID); err != nil || got.TotalTokens != 3 {
		t.Fatalf("repaired monthly cutover %+v %v", got, err)
	}
}

func TestWholeBranchAllObservationMonthWrites(t *testing.T) {
	s := auditFullFixture(t)
	pool := s.Driver().(*sql.DB)
	ctx := context.Background()
	now := time.Now().UTC()
	history := historymaintenance.New(pool, nil, nil)
	ConfigureHistoryReader(s, history)
	defer ConfigureHistoryReader(s, nil)
	if err := history.PrepareMonths(ctx, now); err != nil {
		t.Fatal(err)
	}
	owner := 1
	chat := &models.ChatMessageRecord{UserID: &owner, TraceID: "month-chat", SessionID: "month-session", Role: "assistant", Content: "chat", CreatedAt: now}
	if err := NewChatMessageRepository(s).Create(chat); err != nil {
		t.Fatal(err)
	}
	risk := &models.RiskHit{UserID: &owner, TraceID: "month-risk", RuleID: "r", RuleName: "r", Severity: "high", Action: "block", MatchSummary: "risk", CreatedAt: now}
	if err := NewRiskHitRepository(s).Create(risk); err != nil {
		t.Fatal(err)
	}
	cost := &models.CostRecord{UserID: &owner, TraceID: "month-cost", ProviderType: "openai", ModelName: "m", Currency: "USD", EstimatedCost: 1.234567, RecordedAt: now}
	if err := NewCostRecordRepository(s).Create(cost); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		kind string
		id   int
	}{{"chat_messages", chat.ID}, {"risk_hits", risk.ID}, {"cost_records", cost.ID}} {
		name := row.kind + "_" + now.UTC().Format("200601")
		var registered, n int
		if err := pool.QueryRow(`SELECT COUNT(*) FROM history_month_catalog WHERE table_name=?`, name).Scan(&registered); err != nil {
			t.Fatal(err)
		}
		if registered != 1 {
			t.Errorf("%s has no registered display month", row.kind)
			continue
		}
		if err := pool.QueryRow("SELECT COUNT(*) FROM `"+name+"` WHERE original_id=?", row.id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s new source was not routed into registered month", row.kind)
		}
		if _, complete, err := history.VerifyLogMonth(ctx, name, 0); err != nil || !complete {
			t.Fatalf("%s verification %v %v", row.kind, complete, err)
		}
		if err := history.CutoverLogMonth(ctx, name); err != nil {
			t.Fatal(err)
		}
		record, err := history.Get(ctx, row.kind, int64(row.id), 1, false)
		if err != nil {
			t.Fatal(err)
		}
		switch row.kind {
		case "chat_messages":
			var value models.ChatMessageRecord
			if err = json.Unmarshal(record.Payload, &value); err != nil || value.Content != "chat" {
				t.Fatalf("typed chat %+v %v", value, err)
			}
		case "risk_hits":
			var value models.RiskHit
			if err = json.Unmarshal(record.Payload, &value); err != nil || value.MatchSummary != "risk" {
				t.Fatalf("typed risk %+v %v", value, err)
			}
		case "cost_records":
			var value models.CostRecord
			if err = json.Unmarshal(record.Payload, &value); err != nil || value.EstimatedCost != 1.234567 {
				t.Fatalf("typed display cost %+v %v", value, err)
			}
		}
	}
	if items, err := NewChatMessageRepository(s).ListByTraceID("month-chat"); err != nil || len(items) != 1 || items[0].Content != "chat" {
		t.Fatalf("routed chat list %+v %v", items, err)
	}
	if items, err := NewRiskHitRepository(s).ListByTraceID("month-risk"); err != nil || len(items) != 1 || items[0].MatchSummary != "risk" {
		t.Fatalf("routed risk list %+v %v", items, err)
	}
	if items, err := NewCostRecordRepository(s).ListByTraceID("month-cost"); err != nil || len(items) != 1 || items[0].EstimatedCost != 1.234567 {
		t.Fatalf("routed display cost list %+v %v", items, err)
	}
	var ledger int
	if err := pool.QueryRow(`SELECT COUNT(*) FROM cost_records WHERE id=?`, cost.ID).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatal("monthly routing removed authoritative financial source")
	}
	for _, kind := range []string{"chat_messages", "risk_hits"} {
		held, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		query := `INSERT INTO chat_messages(user_id,trace_id,session_id,role,content,sequence_no,created_at)VALUES(1,'month-chat','month-session','assistant','legacy',1,?)`
		if kind == "risk_hits" {
			query = `INSERT INTO risk_hits(user_id,trace_id,rule_id,rule_name,severity,action,match_summary,created_at)VALUES(1,'month-risk','r','r','high','block','legacy',?)`
		}
		if _, err = held.ExecContext(ctx, query, now); err != nil {
			held.Rollback()
			t.Fatal(err)
		}
		if _, err = pool.ExecContext(ctx, query, now); err != nil {
			held.Rollback()
			t.Fatal(err)
		}
		if n, err := history.CatchupLogs(ctx, kind, 1); err != nil || n != 1 {
			held.Rollback()
			t.Fatalf("higher committed catchup %s %d %v", kind, n, err)
		}
		if err = held.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err = history.Get(ctx, kind, 1, 1, false); !errors.Is(err, historymaintenance.ErrMonthlyRouteIncomplete) {
			t.Fatalf("late lower ID route silently served: %s %v", kind, err)
		}
		if n, err := history.CatchupLogs(ctx, kind, 1); err != nil || n != 1 {
			t.Fatalf("late lower ID catchup %s %d %v", kind, n, err)
		}
		trace := "month-chat"
		if kind == "risk_hits" {
			trace = "month-risk"
		}
		page, err := history.ListPage(ctx, historymaintenance.Query{Kind: kind, TraceID: trace, Exact: true, OwnerID: 1, Limit: 200})
		if err != nil || len(page.Records) != 3 {
			t.Fatalf("commit-visible month catchup %s %d %v", kind, len(page.Records), err)
		}
	}
}

func TestWholeBranchActiveMonthBodyMigrationAndExpiry(t *testing.T) {
	s := auditFullFixture(t)
	pool := s.Driver().(*sql.DB)
	ctx := context.Background()
	at := time.Now().UTC().AddDate(0, 0, -40).Truncate(time.Second)
	history := historymaintenance.New(pool, nil, nil)
	ConfigureHistoryReader(s, history)
	defer ConfigureHistoryReader(s, nil)
	if err := history.PrepareMonths(ctx, at); err != nil {
		t.Fatal(err)
	}
	owner := 1
	body := strings.Repeat("large private legacy body ", 400)
	v := &models.ModelInvocation{UserID: &owner, TraceID: "monthly-migrate", RequestID: "monthly-migrate", ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: at, CompletedAt: &at, RequestPayload: &body}
	repo := NewModelInvocationRepository(s)
	if err := repo.Create(v); err != nil {
		t.Fatal(err)
	}
	month, _ := historymaintenance.MonthlyTable("model_invocations", at)
	if _, complete, err := history.VerifyLogMonth(ctx, month, 0); err != nil || !complete {
		t.Fatalf("verify %v %v", complete, err)
	}
	if err := history.CutoverLogMonth(ctx, month); err != nil {
		t.Fatal(err)
	}
	memory := &failingObservationObjects{memory: observationObjects{}}
	key := bytes.Repeat([]byte{8}, 32)
	objects, err := observability.NewEncryptedObjects(memory, key)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := observability.NewJournal(t.TempDir(), key, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	undo := ConfigureObservability(s, journal, objects)
	defer undo()
	if _, n, err := MigrateObservationBodies(ctx, "model_invocations", 0, 500); err != nil || n != 1 {
		t.Fatalf("active body migration %d %v", n, err)
	}
	var payload string
	if err = pool.QueryRow("SELECT payload_json FROM `"+month+"` WHERE original_id=?", v.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "large private legacy body") || !strings.Contains(payload, "request_body_ref") {
		t.Fatal("active month retained plaintext after source body migration")
	}
	got, err := repo.GetByID(v.ID)
	if err != nil || got.RequestPayload == nil || *got.RequestPayload != body || got.BodyStatus["RequestPayload"] != "available" {
		t.Fatalf("active migrated hydration %+v %v", got, err)
	}
	// Native proof acquisition is covered by the separate actual backup pipeline;
	// here seed its verified gate state to exercise physical expiry and serving.
	for _, q := range []string{`UPDATE observability_body_refs SET created_at=UTC_TIMESTAMP()-INTERVAL 40 DAY`, `UPDATE observability_objects SET retain_until=UTC_TIMESTAMP()-INTERVAL 1 DAY,backup_verified_at=UTC_TIMESTAMP(),restore_verified_at=UTC_TIMESTAMP()`} {
		if _, err = pool.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := ReclaimObservationObjects(ctx, 500); err != nil || n != 1 {
		t.Fatalf("verified body expiry %d %v", n, err)
	}
	got, err = repo.GetByID(v.ID)
	if err != nil || got.RequestPayload != nil || got.BodyStatus["RequestPayload"] != "expired" {
		t.Fatalf("monthly expiry resurrected plaintext %+v %v", got, err)
	}
}

func TestWholeBranchMonthlyTraceRequiresAtMostFourMonths(t *testing.T) {
	s := auditFullFixture(t)
	pool := s.Driver().(*sql.DB)
	ctx := context.Background()
	history := historymaintenance.New(pool, nil, nil)
	ConfigureHistoryReader(s, history)
	defer ConfigureHistoryReader(s, nil)
	owner := 1
	first := time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		at := first.AddDate(0, i, 0)
		if err := history.PrepareMonths(ctx, at); err != nil {
			t.Fatal(err)
		}
		v := &models.ModelInvocation{UserID: &owner, TraceID: "five-month-trace", RequestID: fmt.Sprintf("month-%d", i), ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: at, CompletedAt: &at}
		if err := NewModelInvocationRepository(s).Create(v); err != nil {
			t.Fatal(err)
		}
		name, _ := historymaintenance.MonthlyTable("model_invocations", at)
		if _, complete, err := history.VerifyLogMonth(ctx, name, 0); err != nil || !complete {
			t.Fatalf("verify %v %v", complete, err)
		}
		if err := history.CutoverLogMonth(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewModelInvocationRepository(s).ListByTraceID("five-month-trace"); err == nil || !strings.Contains(err.Error(), "4 months") {
		t.Fatalf("unbounded exact trace accepted: %v", err)
	}
	tracer := NewModelInvocationRepository(s).(interface {
		TracePage(context.Context, string, string, int) (*TracePageResult, error)
	})
	if _, err := tracer.TracePage(ctx, "five-month-trace", "", 200); err == nil || !strings.Contains(err.Error(), "4 months") {
		t.Fatalf("paged unbounded trace accepted: %v", err)
	}
	page, err := history.ListPage(ctx, historymaintenance.Query{Kind: "model_invocations", OwnerID: 1, Since: first.Add(-time.Hour), Until: first.AddDate(0, 3, 0).Add(time.Hour), Limit: 200})
	if err != nil || len(page.Records) != 4 {
		t.Fatalf("bounded four-month history %d %v", len(page.Records), err)
	}
}

func TestWholeBranchFirstMonthRegistrationWaitsForProjectedSourceCommit(t *testing.T) {
	for _, kind := range []string{"model_invocations", "audit_events", "cost_records", "chat_messages", "risk_hits"} {
		for _, registered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/registered_first=%v", kind, registered), func(t *testing.T) {
				testWholeBranchRegistrationOrder(t, kind, registered)
			})
		}
	}
}

func testWholeBranchRegistrationOrder(t *testing.T, kind string, registered bool) {
	s := auditFullFixture(t)
	pool := s.Driver().(*sql.DB)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	history := historymaintenance.New(pool, nil, nil)
	at := time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC)
	var database string
	if err := pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	owner := 1
	makeValue := func(trace string) any {
		switch kind {
		case "model_invocations":
			return &models.ModelInvocation{UserID: &owner, TraceID: trace, RequestID: trace, ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: at, CompletedAt: &at}
		case "audit_events":
			return &models.AuditEvent{UserID: &owner, TraceID: trace, EventType: "registration", Message: trace, CreatedAt: at}
		case "cost_records":
			return &models.CostRecord{UserID: &owner, TraceID: trace, ProviderType: "openai", ModelName: "m", Currency: "USD", RecordedAt: at}
		case "chat_messages":
			return &models.ChatMessageRecord{UserID: &owner, TraceID: trace, SessionID: trace, Role: "assistant", Content: trace, CreatedAt: at}
		default:
			return &models.RiskHit{UserID: &owner, TraceID: trace, RuleID: "r", RuleName: "r", Severity: "high", Action: "block", MatchSummary: trace, CreatedAt: at}
		}
	}
	if registered {
		if err := history.PrepareMonths(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	ready := make(chan error, 1)
	allocated := make(chan error, 1)
	project := make(chan struct{})
	commit := make(chan struct{})
	held := make(chan error, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(commit) }) }
	defer release()
	go func() {
		held <- s.WithContext(ctx).Tx(func(tx db.Session) error {
			v := makeValue("registration-low")
			res, err := tx.Collection(kind).Insert(v)
			if err != nil {
				allocated <- err
				return err
			}
			id := res.ID().(int64)
			reflect.ValueOf(v).Elem().FieldByName("ID").SetInt(id)
			allocated <- nil
			select {
			case <-project:
			case <-ctx.Done():
				return ctx.Err()
			}
			// This is the actual source projection hook, including summary/audit,
			// monthly write and transactional pending-receipt acknowledgement.
			if kind == "risk_hits" {
				err = historymaintenance.WriteLogBatch(ctx, monthlySQL{tx.SQL()}, kind, []any{id})
				if err == nil {
					err = historymaintenance.ClearLogPending(ctx, monthlySQL{tx.SQL()}, kind, int(id))
				}
			} else {
				err = maintainObservationSummary(ctx, tx, kind, v)
			}
			ready <- err
			if err != nil {
				return err
			}
			select {
			case <-commit:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	if err := <-allocated; err != nil {
		t.Fatal(err)
	}
	var createErr error
	switch v := makeValue("registration-high").(type) {
	case *models.ModelInvocation:
		createErr = NewModelInvocationRepository(s).Create(v)
	case *models.AuditEvent:
		createErr = NewAuditEventRepository(s).Create(v)
	case *models.CostRecord:
		createErr = NewCostRecordRepository(s).Create(v)
	case *models.ChatMessageRecord:
		createErr = NewChatMessageRepository(s).Create(v)
	case *models.RiskHit:
		createErr = NewRiskHitRepository(s).Create(v)
	}
	if createErr != nil {
		t.Fatal(createErr)
	}
	close(project)
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := history.BackfillLogs(ctx, kind, at, at.Add(time.Hour), 0, 500)
		name, _ := historymaintenance.MonthlyTable(kind, at)
		if err == nil {
			_, _, err = history.VerifyLogMonth(ctx, name, 0)
		}
		if err == nil {
			err = history.CutoverLogMonth(ctx, name)
		}
		done <- err
	}()
	var completed bool
	var backfillErr error
	for !completed {
		select {
		case backfillErr = <-done:
			completed = true
		default:
		}
		if completed {
			break
		}
		var registering int
		// Observe an actual engine wait, not elapsed time or a query that might
		// finish before the held source commits. Registration-first may wait on
		// the monthly table MDL acquired by its already-running source writer.
		if err := pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.processlist p
 LEFT JOIN information_schema.innodb_trx t ON t.trx_mysql_thread_id=p.ID
 WHERE p.DB=DATABASE() AND (t.trx_state='LOCK WAIT' OR p.STATE='Waiting for table metadata lock')`).Scan(&registering); err != nil {
			t.Fatal(err)
		}
		if registering > 0 {
			break
		}
		// This fixture disables performance_schema. INNODB_TRX may retain its
		// previous cache while polled, so also inspect the current engine wait.
		var engine, section, status string
		if err := pool.QueryRowContext(ctx, "SHOW ENGINE INNODB STATUS").Scan(&engine, &section, &status); err != nil {
			t.Fatal(err)
		}
		if start := strings.Index(status, "TRANSACTIONS\n"); start >= 0 {
			status = status[start:]
			if end := strings.Index(status, "FILE I/O"); end >= 0 {
				status = status[:end]
			}
			if strings.Contains(status, "index uk_history_kind_month of table `"+database+"`.`history_month_catalog`") && strings.Contains(status, "insert intention waiting") {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	release()
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if !completed {
		backfillErr = <-done
	}
	if backfillErr != nil {
		t.Fatal(backfillErr)
	}
	page, err := history.ListPage(ctx, historymaintenance.Query{Kind: kind, OwnerID: 1, Since: at, Until: at.Add(time.Hour), Limit: 200})
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("first-registration cutover omitted committed lower ID after projection acknowledgement: records=%d error=%v", len(page.Records), err)
	}
	if page.Records[0].ID != 1 || page.Records[1].ID != 2 {
		t.Fatal("registration changed stable public identities")
	}
	queue := map[string]string{"model_invocations": "observation_invocation_pending", "audit_events": "observation_audit_pending", "cost_records": "observation_cost_pending", "chat_messages": "observation_chat_pending", "risk_hits": "observation_risk_pending"}[kind]
	var pending int
	if err := pool.QueryRow("SELECT COUNT(*) FROM " + queue).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("unexpected pending receipts %d %v", pending, err)
	}
}
