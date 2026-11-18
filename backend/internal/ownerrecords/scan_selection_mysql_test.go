package ownerrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

func scanSelectionTestDB(t *testing.T) db.Session {
	t.Helper()
	host := os.Getenv("NB_KEYS_TEST_MYSQL")
	if host == "" {
		t.Skip("local disposable MySQL not configured")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatal("localhost required")
	}
	settings := mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("NB_KEYS_TEST_PASSWORD"), Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(settings)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("owner_scan_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	settings.Database = name
	session, err := mysql.Open(settings)
	if err != nil {
		t.Fatal(err)
	}
	session.SetMaxOpenConns(4)
	session.SetMaxIdleConns(4)
	t.Cleanup(func() { session.Close() })
	return session
}

func TestScanSelectionMySQLCapacity(t *testing.T) {
	session := scanSelectionTestDB(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := session.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile(filepath.Join("..", "db", "migrations", "067_add_northbound_owner_records.sql"))
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	exec(`CREATE TABLE instances(id BIGINT PRIMARY KEY,user_id BIGINT,owner VARCHAR(128),type VARCHAR(32),instance_mode VARCHAR(16),status VARCHAR(32),workspace_path TEXT,access_token VARCHAR(255))`)
	exec(`CREATE TABLE model_invocations(id BIGINT PRIMARY KEY,instance_id BIGINT,session_id VARCHAR(255),created_at DATETIME(6),synthetic_padding TEXT,KEY idx_model_invocations_created_at(created_at),KEY idx_model_invocations_instance_session(instance_id,session_id,created_at))`)
	exec(`CREATE TABLE fixture_digits(n INT PRIMARY KEY)`)
	exec(`INSERT INTO fixture_digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`)
	numbers := `SELECT a.n+10*b.n+100*c.n+1000*d.n+10000*e.n+100000*f.n n FROM fixture_digits a CROSS JOIN fixture_digits b CROSS JOIN fixture_digits c CROSS JOIN fixture_digits d CROSS JOIN fixture_digits e CROSS JOIN fixture_digits f`
	exec(`INSERT INTO instances SELECT 21000+n,99,CONCAT('capacity-',n,'@example.test'),'openclaw','lite','running',CONCAT('/unused/openclaw/user-99/instance-',21000+n),'synthetic-generation' FROM (` + numbers + `) nums WHERE n<3200`)
	// Historical bulk and skewed recent activity, not 150k copies of today's calls.
	exec(`INSERT INTO model_invocations SELECT 1000000+n,21000+(n%3200),'old-session',UTC_TIMESTAMP()-INTERVAL (2+n%30) DAY,REPEAT('synthetic',32) FROM (` + numbers + `) nums WHERE n<180000`)
	exec(`INSERT INTO model_invocations SELECT 2000000+n,22039+(n%11),'recent-session',UTC_TIMESTAMP()-INTERVAL 2 MINUTE,'synthetic' FROM (` + numbers + `) nums WHERE n<568`)
	exec(`INSERT INTO model_invocations VALUES(3000000,21030,'future',UTC_TIMESTAMP()+INTERVAL 1 DAY,'synthetic')`)
	store := &Store{Session: session}
	count := func() int {
		t.Helper()
		row, e := session.SQL().QueryRow(`SELECT COUNT(*) FROM northbound_owner_records`)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = row.Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	selectBoth := func(want int) {
		t.Helper()
		before := count()
		for _, lane := range []string{"recent", "history"} {
			ctx, run := newSyncRun(context.Background(), time.Now())
			started := time.Now()
			items, e := store.scheduledScans(ctx, lane)
			if e != nil || len(items) != want {
				for _, group := range run.counts[lane].Selection {
					if group.State != "success" {
						row, planErr := session.SQL().QueryRow("EXPLAIN FORMAT=JSON "+scheduledScanQuery(group.Name == "active"), lane+"_schedule", 16)
						if planErr == nil {
							var plan string
							if row.Scan(&plan) == nil {
								t.Logf("failed %s plan: %s", group.Name, plan)
							}
						}
					}
				}
				t.Fatal(lane, "selection", len(items), e)
			}
			for _, item := range items {
				if item.identity.Instance == 21030 && want == 15 {
					t.Fatal("future invocation counted as active")
				}
			}
			if len(run.counts[lane].Selection) != 2 {
				t.Fatal("group diagnosis missing")
			}
			for _, group := range run.counts[lane].Selection {
				if group.State != "success" || group.Elapsed >= scanSelectionGroupBudget.Milliseconds() {
					t.Fatal("selection exceeded bounded group budget", group)
				}
			}
			t.Logf("%s: %d selected in %s, groups=%+v", lane, len(items), time.Since(started), run.counts[lane].Selection)
		}
		if count() != before {
			t.Fatal("selection attempted to write statistics")
		}
	}
	selectBoth(15) // 11 active and 4 quiet, with no prior schedule rows.
	for _, active := range []bool{true, false} {
		row, e := session.SQL().QueryRow("EXPLAIN FORMAT=JSON "+scheduledScanQuery(active), "recent_schedule", 16)
		if e != nil {
			t.Fatal(e)
		}
		var raw string
		if e = row.Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var plan any
		if e = json.Unmarshal([]byte(raw), &plan); e != nil {
			t.Fatal(e)
		}
		checked := 0
		var walk func(any)
		walk = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if v["table_name"] == "q" {
					parts, ok := v["used_key_parts"].([]any)
					if v["key"] != "idx_owner_record_binding" || !ok || len(parts) != 2 || parts[1] != "instance_id" {
						t.Fatal("schedule lookup is not bounded to one instance", v)
					}
				}
				// The recent-day materialization must be a bounded index range.
				if _, materialized := v["materialized_from_subquery"]; materialized {
					body, _ := json.Marshal(v["materialized_from_subquery"])
					if !strings.Contains(string(body), `"key":"idx_model_invocations_created_at"`) || strings.Contains(string(body), `"access_type":"ALL"`) {
						t.Fatal("invocation materialization regressed to full scan", string(body))
					}
					checked++
				}
				for _, nested := range v {
					walk(nested)
				}
			case []any:
				for _, nested := range v {
					walk(nested)
				}
			}
		}
		walk(plan)
		if !active && checked == 0 {
			t.Fatal("quiet anti-join plan not inspected")
		}
	}
	for _, lane := range []string{"recent", "history"} {
		exec(`INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status,occurred_at,expires_at,source_key,payload) SELECT ?,SHA2(CONCAT('["',?,'","',CAST(i.id AS CHAR),'"]'),256),99,i.owner,i.id,'ready',UTC_TIMESTAMP()-INTERVAL 1 MINUTE,UTC_TIMESTAMP()+INTERVAL 30 MINUTE,SHA2(CONCAT(i.workspace_path,'|',i.access_token),256),JSON_OBJECT('reason','') FROM instances i`, lane+"_schedule", lane+"-schedule")
	}
	selectBoth(0) // No due work is a recorded successful zero, not missing counts.
	ctx, diagnostic := newSyncRun(context.Background(), time.Now())
	items, e := store.scheduledScans(ctx, "history")
	if e != nil {
		t.Fatal(e)
	}
	recordSyncSelected(ctx, "history", len(items))
	diagnostic.stage("history", time.Now(), nil)
	store.saveSyncRun(ctx, diagnostic)
	page, e := store.SyncRecords(ctx, SyncQuery{Kind: "sync_batch", Limit: 1}, 0)
	if e != nil || len(page.Records) != 1 {
		t.Fatal("saved selection diagnostics missing", e)
	}
	if len(page.Records[0].Details) != 0 {
		t.Fatal("list response unnecessarily includes diagnostic payload")
	}
	page, e = store.SyncRecords(ctx, SyncQuery{Kind: "sync_batch", Limit: 1}, page.Records[0].ID)
	if e != nil || len(page.Records) != 1 {
		t.Fatal("saved selection diagnostic detail missing", e)
	}
	var saved struct {
		Stages []SyncStage `json:"stages"`
	}
	if e = json.Unmarshal(page.Records[0].Details, &saved); e != nil {
		t.Fatal(e)
	}
	if len(saved.Stages) != 1 || !saved.Stages[0].SelectedKnown || len(saved.Stages[0].Selection) != 2 || saved.Stages[0].Selected != 0 {
		t.Fatal("known zero/group metadata lost in database", saved)
	}
	var legacy struct {
		Stages []struct {
			Name, State string
			Selected    int
		}
	}
	if e = json.Unmarshal(page.Records[0].Details, &legacy); e != nil || len(legacy.Stages) != 1 || legacy.Stages[0].State != "success" {
		t.Fatal("older JSON reader compatibility", e, legacy)
	}
	exec(`UPDATE northbound_owner_records SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE record_type IN ('recent_schedule','history_schedule')`)
	selectBoth(15)
	// Empty histories should wake on new calls without scanning old invocations.
	exec(`UPDATE northbound_owner_records SET status='empty',occurred_at=UTC_TIMESTAMP()-INTERVAL 3 MINUTE,expires_at=UTC_TIMESTAMP()+INTERVAL 30 MINUTE WHERE record_type IN ('recent_schedule','history_schedule')`)
	selectBoth(11)
	// Unchanged ready histories use a quiet interval too, but new activity must
	// wake them through the same covering instance index as an empty source.
	exec(`UPDATE northbound_owner_records SET status='ready' WHERE record_type IN ('recent_schedule','history_schedule')`)
	selectBoth(11)
	// Generation/owner changes must still invalidate a future schedule.
	exec(`UPDATE instances SET access_token='new-synthetic-generation' WHERE id=21020`)
	exec(`UPDATE instances SET owner='new-owner@example.test' WHERE id=21021`)
	items, e = store.scheduledScans(context.Background(), "history")
	if e != nil {
		t.Fatal(e)
	}
	ids := []uint64{}
	for _, item := range items {
		ids = append(ids, item.identity.Instance)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) != 13 || ids[0] != 21020 || ids[1] != 21021 {
		t.Fatal("changed identities omitted", ids)
	}
	// Office-hour activity growth must not turn a range lookup into a full scan.
	exec(`INSERT INTO model_invocations SELECT 4000000+n,21950+(n%100),'peak-session',UTC_TIMESTAMP()-INTERVAL 1 MINUTE,'synthetic' FROM (` + numbers + `) nums WHERE n<5680`)
	exec(`UPDATE northbound_owner_records SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE record_type IN ('recent_schedule','history_schedule')`)
	selectBoth(20)
	// A missing index is a clear selection failure, never an unbounded fallback.
	exec(`ALTER TABLE model_invocations DROP INDEX idx_model_invocations_created_at`)
	items, e = store.scheduledScans(context.Background(), "history")
	if e == nil || len(items) != 0 {
		t.Fatal("missing time index silently accepted", len(items), e)
	}
}

// Exercise both real worker callers: a slow quiet-group SELECT must not prevent
// the active instance's durable checkpoint. The delay exists only in this local
// test view; no production hooks or background goroutines are introduced.
func TestScanSelectionMySQLPartialWorker(t *testing.T) {
	session := scanSelectionTestDB(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := session.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile(filepath.Join("..", "db", "migrations", "067_add_northbound_owner_records.sql"))
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	exec(`CREATE TABLE fixture_instances(id BIGINT PRIMARY KEY,user_id BIGINT,owner VARCHAR(128),type VARCHAR(32),instance_mode VARCHAR(16),status VARCHAR(32),workspace_path TEXT,access_token VARCHAR(255))`)
	exec(`CREATE TABLE model_invocations(id BIGINT PRIMARY KEY,instance_id BIGINT,session_id VARCHAR(255),created_at DATETIME(6),KEY idx_model_invocations_created_at(created_at),KEY idx_model_invocations_instance_session(instance_id,session_id,created_at))`)
	exec(`CREATE ALGORITHM=MERGE VIEW instances AS SELECT id,user_id,owner,type,instance_mode,status,CASE WHEN id=10 THEN workspace_path ELSE CONCAT(workspace_path,SLEEP(4)) END workspace_path,access_token,1 runtime_generation FROM fixture_instances`)
	root := t.TempDir()
	workspace := filepath.Join(root, "openclaw", "user-99", "instance-10")
	dir := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "sessions")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "synthetic.jsonl"), []byte("{\"type\":\"session\",\"id\":\"synthetic\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{10, 11, 12, 13, 14} {
		exec(`INSERT INTO fixture_instances VALUES(?,99,'partial@example.test','openclaw','lite','running',?,'synthetic-generation')`, id, workspace)
	}
	exec(`INSERT INTO model_invocations VALUES(1,10,'synthetic',UTC_TIMESTAMP()-INTERVAL 1 SECOND)`)
	store := &Store{Session: session, HistoryRoot: root}
	for _, lane := range []string{"recent", "history"} {
		parent, cancel := context.WithTimeout(context.Background(), nativeScanBudget)
		ctx, run := newSyncRun(parent, time.Now())
		var cursor uint64
		if lane == "recent" {
			cursor, err = store.scanRecentQuestions(ctx, 0, map[uint64]string{})
		} else {
			cursor, err = store.ScanHistories(ctx, 0)
		}
		run.stage(lane, time.Now(), err)
		if parent.Err() != nil || cursor != 10 || run.Stages[0].State != "partial" || run.Stages[0].Selected != 1 || run.Stages[0].Processed != 1 || run.Stages[0].Failed != 0 {
			t.Fatal("worker lost active progress", lane, cursor, err, parent.Err(), run.Stages)
		}
		cancel()
		row, e := session.SQL().QueryRow(`SELECT status FROM northbound_owner_records WHERE record_type=? AND instance_id=10`, lane+"_schedule")
		if e != nil {
			t.Fatal(e)
		}
		var state string
		if e = row.Scan(&state); e != nil || state != "ready" {
			t.Fatal("durable successful checkpoint lost", lane, state, e)
		}
	}
}
