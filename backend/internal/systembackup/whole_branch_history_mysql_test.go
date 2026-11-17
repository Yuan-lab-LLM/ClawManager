package systembackup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	appdb "clawreef/internal/db"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"clawreef/internal/systembackup/team"
	"github.com/minio/minio-go/v7"
)

type wholeNativeArchive struct {
	objects *observability.EncryptedObjects
	durable bool
}

func (s wholeNativeArchive) Put(ctx context.Context, owner, key string, raw []byte) ([]byte, error) {
	if s.durable {
		return repository.DurableHistoryArchivePut(ctx, owner, key, raw)
	}
	ref, err := s.objects.Put(ctx, owner, raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ref)
}
func (s wholeNativeArchive) Read(ctx context.Context, owner string, raw []byte) ([]byte, error) {
	var ref observability.BodyRef
	if err := json.Unmarshal(raw, &ref); err != nil {
		return nil, err
	}
	return s.objects.Get(ctx, owner, ref)
}

// Complete migrated source -> actual encrypted Backfill -> summary proof ->
// monthly typed Get/Trace -> native SQL/object capture -> inert isolated restore.
func TestWholeBranchFullSchemaArchiveMonthlyNativeRestore(t *testing.T) {
	source := fullSchemaDB(t)
	sess := appdb.Session
	ctx := context.Background()
	store := testObjectStore(t)
	objects := historyTestObjects{store, "whole-branch/"}
	key := bytes.Repeat([]byte{91}, 32)
	encrypted, err := observability.NewEncryptedObjects(objects, key)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := observability.NewJournal(t.TempDir(), key, observability.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	undo := repository.ConfigureObservability(sess, journal, encrypted)
	defer undo()
	history := historymaintenance.New(source, wholeNativeArchive{encrypted, true}, nil)
	repository.ConfigureHistoryReader(sess, history)
	defer repository.ConfigureHistoryReader(sess, nil)
	at := time.Now().UTC().AddDate(0, 0, -100).Truncate(time.Second)
	until := at.Add(time.Hour)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) { t.Helper(); _, e := source.ExecContext(ctx, q, args...); must(e) }
	exec(`INSERT INTO users(id,username,email,password_hash,role)VALUES(42,'whole-archive','whole-archive@test.invalid','fixture','user')`)
	longError := strings.Repeat("full SQL archive error ", 100)
	exec(`INSERT INTO model_invocations(id,user_id,trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,is_streaming,tokens_per_second,error_message,request_payload,response_payload,created_at,completed_at) WITH RECURSIVE n AS(SELECT 1 id UNION ALL SELECT id+1 FROM n WHERE id<502) SELECT id,42,CONCAT('whole-',id),CONCAT('whole-',id),'auto','model','openai','llm','completed',3,MOD(id,2),IF(id=501,12.3456,NULL),?,'request body','response body',?,? FROM n`, longError, at, at)
	exec(`INSERT INTO audit_events(id,user_id,trace_id,event_type,severity,traffic_class,message,details,created_at)VALUES(601,42,'whole-audit-only','gateway.request.failed','error','llm','audit message survives SQL','audit body',?)`, at)
	exec(`INSERT INTO chat_messages(id,user_id,trace_id,session_id,role,content,sequence_no,created_at)VALUES(602,42,'whole-chat','whole-session','assistant','chat body',1,?)`, at)
	exec(`INSERT INTO risk_hits(id,user_id,trace_id,rule_id,rule_name,severity,action,match_summary,created_at)VALUES(603,42,'whole-risk','r','risk rule','high','block','risk body',?)`, at)
	exec(`INSERT INTO cost_records(id,user_id,trace_id,provider_type,model_name,currency,total_tokens,input_unit_price,output_unit_price,estimated_cost,actual_cost,internal_cost,recorded_at) VALUES(604,42,'whole-cost','openai','model','USD',3,0.125,0.25,0.375,0.5,0.625,?),(605,42,'whole-cost-null','openai','model','USD',3,0,0,0,NULL,0,?)`, at, at)
	exec(`INSERT INTO teams(id,user_id,name,status)VALUES(700,42,'whole-terminal','deleted')`)
	exec(`INSERT INTO team_members(id,team_id,user_id,member_key,display_name,role,status)VALUES(701,700,42,'worker','Worker','worker','deleted')`)
	exec(`INSERT INTO team_tasks(id,team_id,target_member_id,message_id,status,payload_json,result_json,finished_at,created_at)VALUES(702,700,701,'whole-task','succeeded','{"task":"original"}','{"result":"preserved"}',?,?)`, at, at)
	exec(`INSERT INTO team_events(id,team_id,event_type,payload_json,created_at)VALUES(703,700,'terminal','{"event":"preserved"}',?)`, at)
	exec(`INSERT INTO history_replay_closures VALUES(700,?,TRUE,TRUE,TRUE,REPEAT('a',64),?)`, at.Add(time.Hour), at)
	for _, kind := range historymaintenance.Kinds {
		after := int64(0)
		expected := 1
		if kind == "model_invocations" {
			after = 500
			expected = 2
		}
		_, n, e := history.Backfill(ctx, kind, at, until, after, 500)
		must(e)
		if n != expected {
			t.Fatalf("%s actual full-schema archive count %d", kind, n)
		}
	}
	_, err = repository.ReconcileObservationSummaries(ctx, sess, 500)
	must(err)
	var gotError, message string
	must(source.QueryRow(`SELECT error_message FROM observation_invocation_summaries WHERE id=501`).Scan(&gotError))
	if gotError != longError {
		t.Fatal("archive-before-hot500 lost error")
	}
	must(source.QueryRow(`SELECT message FROM observation_audit_event_summaries WHERE id=601`).Scan(&message))
	if message != "audit message survives SQL" {
		t.Fatal("archive-only audit message lost")
	}
	for range 2 {
		_, err = repository.ReconcileObservationSummaries(ctx, sess, 500)
		must(err)
		_, err = repository.ReconcileCostSummaries(ctx, sess, 500)
		must(err)
	}
	for range 10 {
		_, ready, e := repository.ReconcileAuditReports(ctx, sess, 500)
		must(e)
		if ready {
			break
		}
	}
	for _, kind := range []string{"chat_messages", "risk_hits"} {
		_, err = history.CatchupLogs(ctx, kind, 500)
		must(err)
	}
	for _, kind := range []string{"model_invocations", "audit_events", "chat_messages", "risk_hits", "cost_records"} {
		var after int64
		for {
			last, n, e := history.BackfillLogs(ctx, kind, at, until, after, 500)
			must(e)
			after = last
			if n < 500 {
				break
			}
		}
		name, _ := historymaintenance.MonthlyTable(kind, at)
		after = 0
		for {
			last, complete, e := history.VerifyLogMonth(ctx, name, after)
			must(e)
			after = last
			if complete {
				break
			}
		}
		must(history.CutoverLogMonth(ctx, name))
	}
	invocation, err := repository.NewModelInvocationRepository(sess).GetByID(501)
	must(err)
	if !invocation.IsStreaming || invocation.TokensPerSecond == nil || *invocation.TokensPerSecond != 12.3456 || invocation.ErrorMessage == nil || *invocation.ErrorMessage != longError {
		t.Fatalf("typed monthly Get %+v", invocation)
	}
	owner := 42
	written := &models.ModelInvocation{UserID: &owner, TraceID: "new-wal-monthly", RequestID: "new-wal-monthly", ProviderType: "openai", RequestedModel: "m", ActualProviderModel: "m", TrafficClass: "llm", Status: "completed", CreatedAt: at, CompletedAt: &at, TotalTokens: 7}
	must(repository.NewModelInvocationRepository(sess).Create(written))
	if written.ID <= 502 {
		t.Fatal("WAL projection did not retain global public ID allocation")
	}
	newRecord, e := history.Get(ctx, "model_invocations", int64(written.ID), 42, false)
	must(e)
	if newRecord.Status != "monthly" {
		t.Fatal("WAL projection did not serve from active month")
	}
	audit := &models.AuditEvent{UserID: &owner, TraceID: "new-wal-audit-monthly", EventType: "gateway.request.completed", TrafficClass: "llm", Severity: "info", Message: "new audit in active month", CreatedAt: at}
	must(repository.NewAuditEventRepository(sess).Create(audit))
	newRecord, e = history.Get(ctx, "audit_events", int64(audit.ID), 42, false)
	must(e)
	if newRecord.Status != "monthly" {
		t.Fatal("audit WAL projection did not serve from active month")
	}
	tracer := repository.NewModelInvocationRepository(sess).(interface {
		TracePage(context.Context, string, string, int) (*repository.TracePageResult, error)
	})
	page, err := tracer.TracePage(ctx, "whole-501", "", 200)
	must(err)
	if len(page.Records) != 1 {
		t.Fatalf("real Trace %+v", page)
	}
	must(ValidateSchema(ctx, source))
	tables, err := SnapshotBusinessTables(ctx, source)
	must(err)
	var snapshot, normalized, objectSnapshot bytes.Buffer
	_, err = ExportMySQL(ctx, source, &snapshot, tables)
	must(err)
	inventory := newHistoryInventory()
	_, err = ReadSQLSnapshot(bytes.NewReader(snapshot.Bytes()), nil, func(table SQLTable, row team.Row) error { return inventory.record(table, row, 10000) })
	must(err)
	_, err = NormalizeSQLSnapshot(ctx, bytes.NewReader(snapshot.Bytes()), &normalized, time.Now().UTC(), func(SQLTable, team.Change) error { return nil })
	must(err)
	budget := ObjectBudget{MaxBytes: 32 << 20, MaxItems: 1000}
	must(CaptureObjects(ctx, store.Client, &objectSnapshot, []string{store.Bucket}, budget))
	target, targetName := isolatedSchemaDB(t, source)
	must(prepareMonthlyRestoreSchema(ctx, target, bytes.NewReader(normalized.Bytes())))
	var sourceName string
	must(source.QueryRow(`SELECT DATABASE()`).Scan(&sourceName))
	_, err = RestoreMySQL(ctx, target, sourceName, targetName, bytes.NewReader(normalized.Bytes()), nil)
	must(err)
	_, err = VerifyMySQL(ctx, target, bytes.NewReader(normalized.Bytes()), 10000)
	must(err)
	targetBucket, err := RandomID("cm-restore-")
	must(err)
	must(store.Client.MakeBucket(ctx, targetBucket, minio.MakeBucketOptions{}))
	t.Cleanup(func() {
		for object := range store.Client.ListObjects(ctx, targetBucket, minio.ListObjectsOptions{Recursive: true}) {
			_ = store.Client.RemoveObject(ctx, targetBucket, object.Key, minio.RemoveObjectOptions{})
		}
		_ = store.Client.RemoveBucket(ctx, targetBucket)
	})
	destination := ImmutableObjectStore{Client: store.Client, Bucket: targetBucket}
	mapping := map[string]string{store.Bucket: destination.Bucket}
	must(RestoreObjects(ctx, destination.Client, bytes.NewReader(objectSnapshot.Bytes()), budget, mapping))
	secret, _ := json.Marshal(map[string]any{"immutable": true, "data": map[string]string{"master-key": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(key)))}})
	readSnapshot := func(sink func(ObjectMetadata) (io.WriteCloser, error)) error {
		return ReadObjectSnapshot(ctx, bytes.NewReader(objectSnapshot.Bytes()), budget, sink)
	}
	refs := make([]observability.BodyRef, 0, len(inventory.sources))
	for _, dep := range inventory.sources {
		refs = append(refs, dep.Ref)
	}
	_, err = verifyHistoryObjectDependencies(ctx, refs, inventory, secret, readSnapshot, &RestoredDataReader{Objects: destination.Client, BucketMapping: mapping}, budget)
	must(err)
	restoredEncrypted, err := observability.NewEncryptedObjects(historyTestObjects{destination, "whole-branch/"}, key)
	must(err)
	restored := historymaintenance.New(target, wholeNativeArchive{restoredEncrypted, false}, nil)
	for _, tc := range []struct {
		kind  string
		id    int64
		value any
	}{{"model_invocations", 501, &models.ModelInvocation{}}, {"model_invocations", 502, &models.ModelInvocation{}}, {"audit_events", 601, &models.AuditEvent{}}, {"chat_messages", 602, &models.ChatMessageRecord{}}, {"risk_hits", 603, &models.RiskHit{}}, {"team_tasks", 702, &models.TeamTask{}}, {"team_events", 703, &models.TeamEvent{}}} {
		record, e := restored.Get(ctx, tc.kind, tc.id, 42, false)
		must(e)
		must(json.Unmarshal(record.Payload, tc.value))
		if record.ID != tc.id {
			t.Fatal("native restored stable ID changed")
		}
		switch v := tc.value.(type) {
		case *models.ModelInvocation:
			if v.IsStreaming != (v.ID == 501) || v.ErrorMessage == nil || *v.ErrorMessage != longError || (v.ID == 501 && (v.TokensPerSecond == nil || *v.TokensPerSecond != 12.3456)) || (v.ID == 502 && v.TokensPerSecond != nil) {
				t.Fatalf("restored typed invocation %+v", v)
			}
		case *models.AuditEvent:
			if v.Message != "audit message survives SQL" {
				t.Fatal("restored audit message")
			}
		case *models.ChatMessageRecord:
			if v.Content != "chat body" {
				t.Fatal("restored chat")
			}
		case *models.RiskHit:
			if v.MatchSummary != "risk body" {
				t.Fatal("restored risk")
			}
		case *models.TeamTask:
			if !bytes.Contains(record.Payload, []byte("preserved")) {
				t.Fatal("restored terminal result")
			}
		case *models.TeamEvent:
			if !bytes.Contains(record.Payload, []byte("preserved")) {
				t.Fatal("restored terminal event")
			}
		}
	}
	var hooks, active int
	must(target.QueryRow(`SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()`).Scan(&hooks))
	must(target.QueryRow(`SELECT COUNT(*) FROM history_month_catalog WHERE read_route='monthly' OR route_verified=TRUE`).Scan(&active))
	if hooks != 0 || active != 0 {
		t.Fatalf("native restore replayed execution/route authority: %d %d", hooks, active)
	}
	// Financial source rows stay native. Explicit verification can re-enable
	// their display-only month after restore; normalization never does that.
	name, _ := historymaintenance.MonthlyTable("cost_records", at)
	_, complete, e := restored.VerifyLogMonth(ctx, name, 0)
	must(e)
	if !complete {
		t.Fatal("two cost rows did not finish route verification")
	}
	must(restored.CutoverLogMonth(ctx, name))
	for _, id := range []int64{604, 605} {
		r, e := restored.Get(ctx, "cost_records", id, 42, false)
		must(e)
		var cost models.CostRecord
		must(json.Unmarshal(r.Payload, &cost))
		if r.Status != "monthly" || (id == 604 && (cost.InputUnitPrice != 0.125 || cost.OutputUnitPrice != 0.25 || cost.EstimatedCost != 0.375 || cost.ActualCost == nil || *cost.ActualCost != 0.5 || cost.InternalCost != 0.625)) || (id == 605 && cost.ActualCost != nil) {
			t.Fatalf("native restored monthly decimals/null: %+v", cost)
		}
	}
	var ledgerRows int
	must(target.QueryRow(`SELECT COUNT(*) FROM cost_records WHERE id IN(604,605)`).Scan(&ledgerRows))
	if ledgerRows != 2 {
		t.Fatal("native financial source changed")
	}
	t.Log("ACTUAL full migrations: all six archive kinds and five active monthly log kinds, bool true/false, every cost decimal/null, archive beyond hot500, summaries, monthly Get/Trace, SQL+encrypted objects, typed isolated restore, inert hooks/routes")
}
