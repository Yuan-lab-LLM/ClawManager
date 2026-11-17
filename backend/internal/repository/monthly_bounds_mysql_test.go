package repository

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"clawreef/internal/historymaintenance"
)

func TestWholeBranchMonthlyListBoundsInlinePayloadWorkingSet(t *testing.T) {
	s := auditFullFixture(t)
	pool := s.Driver().(*sql.DB)
	ctx := context.Background()
	history := historymaintenance.New(pool, nil, nil)
	at := time.Now().UTC().Truncate(time.Second)
	if err := history.PrepareMonths(ctx, at); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("B", 2<<20)
	for id := 1; id <= 20; id++ {
		if _, err := pool.Exec(`INSERT INTO model_invocations(id,user_id,trace_id,request_id,provider_type,requested_model,actual_provider_model,traffic_class,status,request_payload,created_at,completed_at)VALUES(?,1,?,?,'openai','m','m','llm','completed',?,?,?)`, id, fmt.Sprint("memory-", id), fmt.Sprint("memory-", id), body, at, at); err != nil {
			t.Fatal(err)
		}
	}
	// Individual normal-size batches establish the supported legacy route;
	// this reader regression does not depend on aggregate-backfill behavior.
	for range 20 {
		if _, err := ReconcileObservationSummaries(ctx, s, 1); err != nil {
			t.Fatal(err)
		}
	}
	name, _ := historymaintenance.MonthlyTable("model_invocations", at)
	if _, complete, err := history.VerifyLogMonth(ctx, name, 0); err != nil || !complete {
		t.Fatalf("verify %v %v", complete, err)
	}
	if err := history.CutoverLogMonth(ctx, name); err != nil {
		t.Fatal(err)
	}
	q := historymaintenance.Query{Kind: "model_invocations", OwnerID: 1, Since: at, Until: at.Add(time.Hour), Limit: 200}
	// The first excluded payload must not be decoded just to establish HasMore.
	if _, err := pool.Exec("UPDATE `" + name + "` SET payload_sha256=REPEAT('0',64) WHERE original_id=4"); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	page, err := history.ListPage(ctx, q)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*historymaintenance.MaxPageBytes {
		t.Fatalf("reader eagerly allocated payloads beyond accepted8MiB prefix: allocated=%d page_bytes=%d records=%d", allocated, page.Bytes, len(page.Records))
	}
	if len(page.Records) != 3 || !page.HasMore || page.NextID != 3 || page.Bytes > historymaintenance.MaxPageBytes {
		t.Fatalf("byte bounded first page rows=%d more=%v cursor=%d bytes=%d", len(page.Records), page.HasMore, page.NextID, page.Bytes)
	}
	q.AfterID = 3
	if _, err := history.ListPage(ctx, q); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("included corrupt payload was not rejected: %v", err)
	}
	if _, err := pool.Exec("UPDATE `" + name + "` SET payload_sha256=SHA2(payload_json,256) WHERE original_id=4"); err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for {
		for _, r := range page.Records {
			if seen[r.ID] {
				t.Fatal("duplicate byte-prefix continuation")
			}
			seen[r.ID] = true
		}
		if !page.HasMore {
			break
		}
		q.AfterID = page.NextID
		page, err = history.ListPage(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 20 {
		t.Fatalf("byte-prefix export continuation omitted rows: %d", len(seen))
	}
	t.Logf("actual20x2MiB legacy inline month: firstpage allocation=%d, complete cursor/export traversal=%d", after.TotalAlloc-before.TotalAlloc, len(seen))
}

func TestWholeBranchScheduledMonthlyCatchupMakesByteBoundedProgress(t *testing.T) {
	for _, preHook := range []bool{false, true} {
		t.Run(fmt.Sprintf("pre_hook=%v", preHook), func(t *testing.T) {
			s := auditFullFixture(t)
			pool := s.Driver().(*sql.DB)
			ctx := context.Background()
			history := historymaintenance.New(pool, nil, nil)
			at := time.Now().UTC().Truncate(time.Second)
			if err := history.PrepareMonths(ctx, at); err != nil {
				t.Fatal(err)
			}
			if preHook {
				if _, err := pool.Exec(`DROP TRIGGER observation_invocation_enqueue`); err != nil {
					t.Fatal(err)
				}
			}
			message := strings.Repeat("E", 20<<10)
			if _, err := pool.Exec(`INSERT INTO model_invocations(id,user_id,trace_id,request_id,provider_type,requested_model,actual_provider_model,traffic_class,status,total_tokens,error_message,created_at,completed_at) WITH RECURSIVE n AS(SELECT 1 id UNION ALL SELECT id+1 FROM n WHERE id<500) SELECT id,1,CONCAT('bytes-',id),CONCAT('bytes-',id),'openai','m','m','llm','completed',3,?,?,? FROM n`, message, at, at); err != nil {
				t.Fatal(err)
			}
			if preHook {
				if _, err := pool.Exec(`CREATE TRIGGER observation_invocation_enqueue AFTER INSERT ON model_invocations FOR EACH ROW INSERT IGNORE INTO observation_invocation_pending(id) VALUES(NEW.id)`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(`UPDATE observation_summary_state SET ready=FALSE,target_id=500 WHERE id=1`); err != nil {
				t.Fatal(err)
			}
			var previous int
			for pass := 0; pass < 5; pass++ {
				n, err := ReconcileObservationSummaries(ctx, s, 500)
				if err != nil {
					t.Fatalf("scheduled-size pass%d made no progress: %v", pass, err)
				}
				var facts, pending, cursor int
				var amount int64
				if err = pool.QueryRow(`SELECT COUNT(*),COALESCE(SUM(total_tokens),0) FROM observation_invocation_summaries`).Scan(&facts, &amount); err != nil {
					t.Fatal(err)
				}
				if err = pool.QueryRow(`SELECT COUNT(*) FROM observation_invocation_pending`).Scan(&pending); err != nil {
					t.Fatal(err)
				}
				if err = pool.QueryRow(`SELECT after_id FROM history_maintenance_cursors WHERE kind='query_summary'`).Scan(&cursor); err != nil {
					t.Fatal(err)
				}
				if facts <= previous || facts-previous != n || cursor != facts || amount != int64(3*facts) || (!preHook && pending != 500-facts) || (preHook && pending != 0) {
					t.Fatalf("incorrect prefix progress pass=%d n=%d facts=%d previous=%d pending=%d cursor=%d tokens=%d", pass, n, facts, previous, pending, cursor, amount)
				}
				if pass == 0 && facts >= 500 {
					t.Fatal("scheduled batch did not honor8MiB source payload cap")
				}
				previous = facts
				if facts == 500 {
					break
				}
			}
			if previous != 500 {
				t.Fatalf("bounded retries stalled at%d", previous)
			}
			if n, err := ReconcileObservationSummaries(ctx, s, 500); err != nil || n != 0 {
				t.Fatalf("completed prefix replay %d %v", n, err)
			}
			name, _ := historymaintenance.MonthlyTable("model_invocations", at)
			var count, bad int
			if err := pool.QueryRow("SELECT COUNT(*) FROM `" + name + "`").Scan(&count); err != nil || count != 500 {
				t.Fatalf("month coverage %d %v", count, err)
			}
			if err := pool.QueryRow(`SELECT COUNT(*) FROM observation_invocation_summaries WHERE LENGTH(error_message)<>?`, len(message)).Scan(&bad); err != nil || bad != 0 {
				t.Fatalf("error preservation %d %v", bad, err)
			}
			if err := pool.QueryRow("SELECT COUNT(*) FROM `"+name+"` WHERE BINARY JSON_UNQUOTE(JSON_EXTRACT(payload_json,'$.error_message'))<>BINARY ?", message).Scan(&bad); err != nil || bad != 0 {
				t.Fatalf("monthly error bytes changed %d %v", bad, err)
			}
			t.Logf("actual500x20KiB TEXT errors, prehook=%v, exact facts/months/receipts/cursor all500", preHook)
		})
	}
}

func TestWholeBranchMonthlySharedConsumersSettleOnlyBytePrefix(t *testing.T) {
	for _, mode := range []string{"audit_initial", "audit_pending", "chat_backfill", "chat_pending"} {
		t.Run(mode, func(t *testing.T) {
			s := auditFullFixture(t)
			pool := s.Driver().(*sql.DB)
			ctx := context.Background()
			history := historymaintenance.New(pool, nil, nil)
			at := time.Now().UTC().Truncate(time.Second)
			if err := history.PrepareMonths(ctx, at); err != nil {
				t.Fatal(err)
			}
			if mode == "audit_pending" {
				for pass := 0; pass < 10; pass++ {
					_, ready, err := ReconcileAuditReports(ctx, s, 500)
					if err != nil {
						t.Fatal(err)
					}
					if ready {
						break
					}
				}
			}
			kind, queue := "chat_messages", "observation_chat_pending"
			query := `INSERT INTO chat_messages(id,user_id,trace_id,session_id,role,content,created_at) WITH RECURSIVE n AS(SELECT 1 id UNION ALL SELECT id+1 FROM n WHERE id<500) SELECT id,1,CONCAT('bytes-',id),'session','assistant',REPEAT('C',IF(MOD(id,2)=0,40000,2000)),? FROM n`
			if strings.HasPrefix(mode, "audit_") {
				kind, queue = "audit_events", "observation_audit_pending"
				query = `INSERT INTO audit_events(id,user_id,trace_id,event_type,traffic_class,severity,message,details,created_at) WITH RECURSIVE n AS(SELECT 1 id UNION ALL SELECT id+1 FROM n WHERE id<500) SELECT id,1,CONCAT('bytes-',id),'completed','llm','info',CONCAT('message-',id),REPEAT('A',IF(MOD(id,2)=0,40000,2000)),? FROM n`
			}
			if _, err := pool.Exec(query, at); err != nil {
				t.Fatal(err)
			}
			name, _ := historymaintenance.MonthlyTable(kind, at)
			var after int64
			previous := 0
			for pass := 0; pass < 12 && previous < 500; pass++ {
				var n int
				var err error
				switch mode {
				case "chat_backfill":
					after, n, err = history.BackfillLogs(ctx, kind, at, at.Add(time.Hour), after, 500)
				case "chat_pending":
					n, err = history.CatchupLogs(ctx, kind, 500)
				default:
					n, _, err = ReconcileAuditReports(ctx, s, 500)
				}
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 && strings.HasPrefix(mode, "audit_") {
					continue
				}
				var count, pending, unmatched int
				if err = pool.QueryRow("SELECT COUNT(*) FROM `" + name + "`").Scan(&count); err != nil {
					t.Fatal(err)
				}
				if err = pool.QueryRow("SELECT COUNT(*) FROM " + queue).Scan(&pending); err != nil {
					t.Fatal(err)
				}
				if n != count-previous || count <= previous || (previous == 0 && count >= 500) {
					t.Fatalf("prefix progress n=%d previous=%d rows=%d", n, previous, count)
				}
				if mode == "chat_backfill" {
					if after != int64(count) || pending != 500 {
						t.Fatalf("backfill cursor=%d count=%d pending=%d", after, count, pending)
					}
				} else {
					if pending != 500-count {
						t.Fatalf("cleared unprocessed IDs: pending=%d rows=%d", pending, count)
					}
					if err = pool.QueryRow("SELECT COUNT(*) FROM `" + name + "` m JOIN " + queue + " p ON p.id=m.original_id").Scan(&unmatched); err != nil || unmatched != 0 {
						t.Fatalf("processed receipt retained %d %v", unmatched, err)
					}
				}
				if strings.HasPrefix(mode, "audit_") {
					if err = pool.QueryRow(`SELECT COUNT(*) FROM observation_audit_event_summaries WHERE message<>CONCAT('message-',id)`).Scan(&unmatched); err != nil || unmatched != 0 {
						t.Fatalf("audit message altered %d %v", unmatched, err)
					}
					if err = pool.QueryRow("SELECT COUNT(*) FROM `" + name + "` WHERE LENGTH(JSON_UNQUOTE(JSON_EXTRACT(payload_json,'$.details')))<>IF(MOD(original_id,2)=0,40000,2000)").Scan(&unmatched); err != nil || unmatched != 0 {
						t.Fatalf("audit legacy body altered %d %v", unmatched, err)
					}
				}
				previous = count
			}
			if previous != 500 {
				t.Fatalf("shared consumer stalled at %d", previous)
			}
			if mode == "chat_backfill" {
				settled := 0
				for pass := 0; pass < 5 && settled < 500; pass++ {
					n, err := history.CatchupLogs(ctx, kind, 500)
					if err != nil {
						t.Fatal(err)
					}
					settled += n
				}
				if settled != 500 {
					t.Fatalf("backfill receipts not independently settled: %d", settled)
				}
			}
			t.Logf("%s: variable2KiB/40KiB sources, exact500 rows and receipt/cursor prefix", mode)
		})
	}
}

func TestWholeBranchMonthlyOversizeRowBlocksOnlyAfterValidPrefix(t *testing.T) {
	s := auditFullFixture(t)
	pool := s.Driver().(*sql.DB)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	history := historymaintenance.New(pool, nil, nil)
	if err := history.PrepareMonths(ctx, at); err != nil {
		t.Fatal(err)
	}
	for id, body := range []string{"small", strings.Repeat("X", 9<<20), "last"} {
		if _, err := pool.Exec(`INSERT INTO model_invocations(id,user_id,trace_id,request_id,provider_type,requested_model,actual_provider_model,traffic_class,status,request_payload,created_at)VALUES(?,1,?,?,'openai','m','m','llm','completed',?,?)`, id+1, fmt.Sprint("oversize-", id), fmt.Sprint("oversize-", id), body, at); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := ReconcileObservationSummaries(ctx, s, 500); err != nil || n != 1 {
		t.Fatalf("valid prefix %d %v", n, err)
	}
	for range 2 {
		if _, err := ReconcileObservationSummaries(ctx, s, 500); err == nil || !strings.Contains(err.Error(), "row exceeds8MiB") {
			t.Fatalf("oversized row silently skipped: %v", err)
		}
	}
	var facts, pending, cursor int
	if err := pool.QueryRow(`SELECT (SELECT COUNT(*) FROM observation_invocation_summaries),(SELECT COUNT(*) FROM observation_invocation_pending),(SELECT after_id FROM history_maintenance_cursors WHERE kind='query_summary')`).Scan(&facts, &pending, &cursor); err != nil || facts != 1 || pending != 2 || cursor != 1 {
		t.Fatalf("oversize blocker facts=%d pending=%d cursor=%d err=%v", facts, pending, cursor, err)
	}
}
