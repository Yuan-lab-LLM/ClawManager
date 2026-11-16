package workspacedashboard

import (
	"context"
	"encoding/json"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceMySQLScopeAccountingAndIsolation(t *testing.T) {
	dsn := os.Getenv("WORKSPACE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("WORKSPACE_TEST_MYSQL_DSN requires empty disposable database")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("refusing non-test database")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true", "charset": "utf8mb4"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = row.Scan(&n); err != nil || n != 0 {
		t.Fatal("requires empty DB", err, n)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"chat_sessions", "workspace_dashboard_observations", "cost_records", "model_invocations", "gateway_runtime_tasks", "team_tasks", "team_members", "teams", "instance_model_activity", "instance_desired_state", "user_quotas", "instances"}
	defer func() {
		for _, table := range tables {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	}()
	for _, q := range []string{
		`CREATE TABLE chat_sessions(id INT PRIMARY KEY,user_id INT,instance_id INT,last_activity_at DATETIME)`,
		`CREATE TABLE instances(id INT PRIMARY KEY,user_id INT,owner VARCHAR(128),name VARCHAR(255),type VARCHAR(32),instance_mode VARCHAR(16),status VARCHAR(32),cpu_cores DOUBLE,memory_gb INT,disk_gb INT,runtime_generation INT,created_at DATETIME,updated_at DATETIME)`,
		`CREATE TABLE teams(id INT PRIMARY KEY,user_id INT,resource_scope VARCHAR(16),name VARCHAR(255),status VARCHAR(32))`,
		`CREATE TABLE team_members(id INT PRIMARY KEY,team_id INT,instance_id INT,user_id INT,availability VARCHAR(32),last_seen_at DATETIME,status VARCHAR(32), FOREIGN KEY(instance_id) REFERENCES instances(id) ON DELETE SET NULL)`,
		`CREATE TABLE team_tasks(id INT PRIMARY KEY,team_id INT,target_member_id INT,status VARCHAR(32),payload_json LONGTEXT,result_json LONGTEXT,error_message TEXT,created_at DATETIME,updated_at DATETIME,finished_at DATETIME)`,
		`CREATE TABLE gateway_runtime_tasks(task_key VARCHAR(64) PRIMARY KEY,instance_id INT,owner_user_id INT,generation INT,session_id VARCHAR(191),task_id VARCHAR(191),revoked BOOLEAN,finished BOOLEAN,updated_at DATETIME)`,
		`CREATE TABLE instance_model_activity(instance_id INT PRIMARY KEY,generation INT,stopped_reason VARCHAR(32))`,
		`CREATE TABLE instance_desired_state(instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32))`,
		`CREATE TABLE user_quotas(user_id INT,max_instances INT,max_cpu_cores DOUBLE,max_memory_gb INT,max_storage_gb INT)`,
		`CREATE TABLE model_invocations(id INT PRIMARY KEY,trace_id VARCHAR(100),user_id INT,instance_id INT,status VARCHAR(32),prompt_tokens INT,completion_tokens INT,total_tokens INT,created_at DATETIME)`,
		`CREATE TABLE cost_records(id INT PRIMARY KEY,invocation_id INT,trace_id VARCHAR(100),user_id INT,instance_id INT,currency VARCHAR(16),input_unit_price DOUBLE,output_unit_price DOUBLE,estimated_cost DOUBLE,actual_cost DOUBLE,recorded_at DATETIME)`,
	} {
		exec(q)
	}
	raw, err := os.ReadFile("../db/migrations/110_add_workspace_dashboard_observations.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			exec(q)
		}
	}
	now := time.Date(2026, 11, 12, 1, 15, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	old := now.AddDate(0, 0, -100)
	for _, v := range []struct {
		id, user int
		owner    any
		status   string
	}{{1, 1, nil, "running"}, {2, 2, nil, "running"}, {3, 1, "north@example.com", "running"}, {4, 1, nil, "stopped"}, {5, 1, nil, "running"}, {6, 1, nil, "running"}} {
		exec("INSERT INTO instances VALUES(?,?,?,?,'openclaw','lite',?,1,2,5,2,?,?)", v.id, v.user, v.owner, "Agent", v.status, old, recent)
	}
	exec("INSERT INTO teams VALUES(10,1,'cm','CM','running'),(11,1,'northbound','hidden','running'),(12,2,'cm','other','running')")
	exec("INSERT INTO team_members VALUES(10,10,1,1,'unknown',?,'busy'),(11,11,6,1,'idle',?,'idle')", recent, recent)
	exec("INSERT INTO instance_model_activity VALUES(4,2,'idle_timeout')")
	exec("INSERT INTO user_quotas VALUES(1,10,20,40,100)")
	exec(`INSERT INTO team_tasks VALUES(10,10,10,'failed','{"title":"Review"}',NULL,'failure',?,?,?),(11,10,10,'succeeded','{"title":"Done"}','{"summary":"ok"}',NULL,?,?,?),(12,11,11,'succeeded','{"title":"hidden"}',NULL,NULL,?,?,?)`, recent, recent, recent, recent, recent, recent, recent, recent, recent)
	exec("INSERT INTO gateway_runtime_tasks VALUES('active',1,1,2,'session','native',0,0,?),('ended',5,1,2,'session','done',0,1,?),('old',5,1,1,'session','old',0,0,?),('revoked',5,1,2,'session','revoked',1,0,?),('expired',5,1,2,'session','expired',0,0,?)", recent, recent, recent, recent, old)
	for _, v := range []struct {
		id, user, instance, tokens int
		status                     string
	}{{1, 1, 1, 10, "succeeded"}, {2, 1, 1, 20, "succeeded"}, {3, 1, 1, 30, "succeeded"}, {4, 1, 1, 1000, "blocked"}, {5, 2, 2, 1000, "succeeded"}, {6, 1, 3, 1000, "succeeded"}, {7, 2, 1, 1000, "succeeded"}} {
		exec("INSERT INTO model_invocations VALUES(?,?,?, ?,?,1,2,?,?)", v.id, "trace", v.user, v.instance, v.status, v.tokens, recent)
	}
	exec("INSERT INTO cost_records VALUES(1,1,'trace',1,1,'USD',1,1,9,NULL,?),(2,1,'trace',1,1,'USD',1,1,9,2,?),(3,1,'trace',1,1,'USD',1,1,9,2,?),(4,2,'trace',1,1,'CNY',1,1,3,NULL,?),(5,3,'trace',1,1,'USD',0,0,0,NULL,?),(6,4,'trace',1,1,'USD',1,1,1000,1000,?)", recent, recent, recent, recent, recent, recent)
	store := NewStore(sess)
	exec("INSERT INTO chat_sessions VALUES(1,1,4,?),(2,2,5,?),(3,1,3,?),(4,1,6,?),(5,1,1,?)", now.Add(-2*time.Minute), now.Add(-time.Second), now.Add(-time.Second), now.Add(-time.Second), old)
	d, err := store.Dashboard(context.Background(), 1, "7d", now)
	if err != nil {
		t.Fatal(err)
	}
	// Assert through JSON so the initial RED demonstrates the missing additive API.
	rawRecent, _ := json.Marshal(d)
	var recentView struct {
		Recent []struct {
			ID   int       `json:"id"`
			Last time.Time `json:"last_used_at"`
			Kind string    `json:"activity_kind"`
		} `json:"recent_agents"`
	}
	_ = json.Unmarshal(rawRecent, &recentView)
	if len(recentView.Recent) != 3 || recentView.Recent[0].ID != 5 || recentView.Recent[1].ID != 1 || recentView.Recent[2].ID != 4 || recentView.Recent[2].Kind != "chat" || !recentView.Recent[2].Last.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("recent use must sort/dedupe real own-CM events, never instance heartbeat: %+v", recentView)
	}
	if d.InstancesTotal != 3 || d.StateCounts["busy"] != 1 || d.StateCounts["sleeping"] != 1 || d.StateCounts["running"] != 1 {
		t.Fatalf("scope/states: %+v %+v", d.Instances, d.StateCounts)
	}
	t.Run("isolated usage scope ranges and attribution", func(t *testing.T) {
		// Unrelated source outages must not affect the isolated card request.
		for _, table := range []string{"chat_sessions", "workspace_dashboard_observations", "team_tasks", "gateway_runtime_tasks", "user_quotas", "instance_model_activity"} {
			exec("RENAME TABLE " + table + " TO " + table + "_usage_offline")
			t.Cleanup(func() { exec("RENAME TABLE " + table + "_usage_offline TO " + table) })
		}
		store.SetCapacityReader(func(context.Context) (map[string]int, error) { t.Error("usage queried capacity"); return nil, nil })
		t.Cleanup(func() { store.SetCapacityReader(nil) })
		exec("INSERT INTO model_invocations VALUES(1300,'hidden-team',1,6,'completed',1,2,9000,?),(1301,'deleted-resource',1,5,'completed',1,2,4,?),(1302,'last-week',1,1,'completed',1,2,7,?)", recent, recent, now.AddDate(0, 0, -10))
		t.Cleanup(func() {
			exec("DELETE FROM model_invocations WHERE id BETWEEN 1300 AND 1399")
			exec("UPDATE instances SET status='running' WHERE id=5")
		})
		exec("UPDATE instances SET status='deleted' WHERE id=5")
		for _, tc := range []struct {
			period           string
			tokens, previous int64
			bins             int
		}{{"today", 64, 0, 10}, {"7d", 64, 7, 7}, {"30d", 71, 0, 30}} {
			view, e := store.Usage(context.Background(), 1, tc.period, now)
			if e != nil || view.Usage.TotalTokens == nil || *view.Usage.TotalTokens != tc.tokens || view.Usage.PreviousTokens == nil || *view.Usage.PreviousTokens != tc.previous || len(view.UsageHistory) != tc.bins || view.Coverage.Usage.Status != "ready" {
				t.Fatalf("range/scope mismatch %s: %+v %v", tc.period, view, e)
			}
			if len(view.Usage.Currencies) != 2 || view.Coverage.Costs.Status != "partial" {
				t.Fatalf("cost accounting mismatch: %+v", view)
			}
		}
		other, e := store.Usage(context.Background(), 2, "7d", now)
		if e != nil || other.Usage.TotalTokens == nil || *other.Usage.TotalTokens != 1000 || len(other.AgentUsage) != 1 || other.AgentUsage[0].InstanceID != 2 {
			t.Fatalf("owner isolation: %+v %v", other, e)
		}
		exec("INSERT INTO model_invocations VALUES(1303,'orphan',1,999,'completed',1,2,500,?)", recent)
		partial, e := store.Usage(context.Background(), 1, "7d", now)
		if e != nil || partial.Usage.TotalTokens == nil || *partial.Usage.TotalTokens != 64 || partial.Usage.PreviousTokens != nil || partial.Coverage.Usage.Status != "partial" || partial.Coverage.Costs.Status != "partial" {
			t.Fatalf("orphan attribution lost: %+v %v", partial, e)
		}
		for _, currency := range partial.Usage.Currencies {
			if currency.Complete || currency.PreviousAmount != nil {
				t.Fatalf("invented cost completeness: %+v", currency)
			}
		}
	})
	t.Run("recent horizon ownership failures and period independence", func(t *testing.T) {
		exec("UPDATE chat_sessions SET last_activity_at=? WHERE id=1", now.AddDate(0, 0, -40))
		view := emptyDashboard("today", now, d.Window)
		store.recent(context.Background(), 1, now, view)
		if len(view.RecentAgents) != 3 || view.RecentAgents[2].ID != 4 || view.Coverage.Recent.Status != "ready" {
			t.Fatalf("recent window incorrectly follows Token period: %+v", view.RecentAgents)
		}
		exec("UPDATE chat_sessions SET last_activity_at=? WHERE id=1", now.AddDate(0, 0, -91))
		view = emptyDashboard("today", now, d.Window)
		store.recent(context.Background(), 1, now, view)
		if len(view.RecentAgents) != 2 {
			t.Fatal("horizon/heartbeat fallback", view.RecentAgents)
		}
		exec("UPDATE chat_sessions SET last_activity_at=? WHERE id=1", now.Add(-2*time.Minute))
		for _, table := range []string{"model_invocations", "chat_sessions", "team_tasks", "gateway_runtime_tasks"} {
			exec("RENAME TABLE " + table + " TO " + table + "_recent_offline")
		}
		view = emptyDashboard("7d", now, d.Window)
		store.recent(context.Background(), 1, now, view)
		if len(view.RecentAgents) != 0 || view.Coverage.Recent.Status != "unavailable" {
			t.Fatal("failed sources fabricated recency", view.RecentAgents, view.Coverage.Recent)
		}
		exec("RENAME TABLE chat_sessions_recent_offline TO chat_sessions")
		view = emptyDashboard("7d", now, d.Window)
		store.recent(context.Background(), 1, now, view)
		if len(view.RecentAgents) != 1 || view.RecentAgents[0].ID != 4 || view.Coverage.Recent.Status != "partial" {
			t.Fatal("source failure poisoned valid recent chat", view.RecentAgents, view.Coverage.Recent)
		}
		for _, table := range []string{"model_invocations", "team_tasks", "gateway_runtime_tasks"} {
			exec("RENAME TABLE " + table + "_recent_offline TO " + table)
		}
	})
	if d.Usage.TotalTokens == nil || *d.Usage.TotalTokens != 60 || *d.Usage.InvocationCount != 3 {
		t.Fatalf("usage %+v", d.Usage)
	}
	if len(d.Usage.Currencies) != 2 {
		t.Fatal(d.Usage.Currencies)
	}
	for _, c := range d.Usage.Currencies {
		if c.Complete || c.PreviousAmount != nil {
			t.Fatal("missing price invented completeness", c)
		}
		if c.Currency == "USD" && (c.Amount == nil || *c.Amount != 2 || c.Estimated) {
			t.Fatal("actual/dedup", c)
		}
		if c.Currency == "CNY" && (c.Amount == nil || *c.Amount != 3 || !c.Estimated) {
			t.Fatal(c)
		}
	}
	if len(d.Tasks) != 4 || d.Tasks[0].Status != "failed" {
		t.Fatalf("tasks %+v", d.Tasks)
	}
	for _, task := range d.Tasks {
		if task.Source == "runtime" && task.Status != "ended" && task.Status != "running" {
			t.Fatal(task)
		}
	}
	if d.Observations[6].Offline == nil || d.Observations[6].Starting == nil || *d.Observations[6].Offline != 0 || *d.Observations[6].Starting != 0 {
		t.Fatal("distinct current states missing")
	}
	if d.Observations[0].Total != nil || d.Observations[6].Total == nil {
		t.Fatal("history filled or current sample missing", d.Observations)
	}
	if len(d.Activity) != 365 || d.Activity[364].Calls != 3 || d.Activity[364].Tasks != 2 {
		t.Fatal("activity", d.Activity[364])
	}
	exec("INSERT INTO model_invocations VALUES(101,'prev-start',1,1,'completed',1,2,7,?),(102,'prev-end',1,1,'completed',1,2,1000,?),(103,'current-start',1,1,'completed',1,2,5,?),(104,'now',1,1,'completed',1,2,2000,?)", d.Window.PreviousStart, d.Window.PreviousEnd, d.Window.Start, d.Window.End)
	boundary, boundaryErr := store.Dashboard(context.Background(), 1, "7d", now)
	if boundaryErr != nil || *boundary.Usage.TotalTokens != 65 || *boundary.Usage.PreviousTokens != 7 || *boundary.UsageHistory[0].Tokens != 5 {
		t.Fatal("SQL period boundary mismatch", boundary, boundaryErr)
	}
	exec("DELETE FROM model_invocations WHERE id>=101")
	for n := 100; n < 360; n++ {
		exec("INSERT INTO instances VALUES(?,1,NULL,'Extra','openclaw','lite','running',1,2,5,2,?,?)", n, old, recent)
	}
	capped, capErr := store.Dashboard(context.Background(), 1, "7d", now)
	if capErr != nil || capped.InstancesTotal != 263 || len(capped.Instances) != 256 || !capped.InstancesTruncated || capped.Capacity.AllocatedCPU != 263 {
		t.Fatal("presentation cap changed summary", capErr)
	}
	if len(capped.RecentAgents) != 3 || capped.RecentAgents[0].ID != 5 || capped.RecentAgents[0].Name != "Agent" {
		t.Fatal("presentation cap hid recent authorized metadata", capped.RecentAgents)
	}
	for id := 100; id < 108; id++ {
		exec("INSERT INTO chat_sessions VALUES(?,1,?,?)", id, id, now.Add(-time.Duration(108-id)*time.Second))
	}
	capRecent := emptyDashboard("today", now, d.Window)
	store.recent(context.Background(), 1, now, capRecent)
	if !capRecent.RecentTruncated || len(capRecent.RecentAgents) != 6 || capRecent.RecentAgents[0].ID != 107 {
		t.Fatal("recent cap/order", capRecent.RecentAgents, capRecent.RecentTruncated)
	}
	exec("DELETE FROM chat_sessions WHERE id>=100")
	exec("DELETE FROM instances WHERE id>=100")
	today, todayErr := store.Dashboard(context.Background(), 1, "today", now)
	if todayErr != nil || len(today.UsageHistory) != 10 || today.UsageHistory[9].Tokens == nil || *today.UsageHistory[9].Tokens != 60 {
		t.Fatal("hourly bucket mismatch", todayErr)
	}
	sampleRow, sampleErr := sess.SQL().QueryRow("SELECT COUNT(*) FROM workspace_dashboard_observations WHERE user_id=1")
	var sampleCount int
	if sampleErr != nil {
		t.Fatal(sampleErr)
	}
	if sampleErr = sampleRow.Scan(&sampleCount); sampleErr != nil || sampleCount != 1 {
		t.Fatal("replica bucket uniqueness", sampleErr, sampleCount)
	}
	other, err := store.Dashboard(context.Background(), 2, "7d", now)
	if err != nil || other.InstancesTotal != 1 {
		t.Fatal("owner isolation", other, err)
	}
	exec("UPDATE instances SET status='deleted' WHERE id=1")
	archived, archivedErr := store.Dashboard(context.Background(), 1, "7d", now)
	if archivedErr != nil || archived.InstancesTotal != 2 || archived.Activity[364].Tasks != 2 || archived.Usage.TotalTokens == nil || *archived.Usage.TotalTokens != 60 {
		t.Fatal("deleted resource erased historical spend", archived, archivedErr)
	}
	exec("UPDATE instances SET status='running' WHERE id=1")
	exec("INSERT INTO model_invocations VALUES(1200,'unverifiable',1,999,'completed',1,2,500,?)", recent)
	unattributed, unattributedErr := store.Dashboard(context.Background(), 1, "7d", now)
	if unattributedErr != nil || unattributed.Coverage.Usage.Status != "partial" || unattributed.Coverage.Costs.Status != "partial" || unattributed.Coverage.Activity.Status != "partial" || unattributed.Usage.PreviousTokens != nil {
		t.Fatal("unverifiable history silently complete", unattributed, unattributedErr)
	}
	for _, currency := range unattributed.Usage.Currencies {
		if currency.Complete || currency.PreviousAmount != nil {
			t.Fatal("unverifiable comparison", currency)
		}
	}
	exec("DELETE FROM model_invocations WHERE id=1200")
	t.Run("task source completion combinations", func(t *testing.T) {
		for _, tc := range []struct {
			name                  string
			teamDown, runtimeDown bool
			user                  int
			coverage              string
			counts                [3]int // failed, succeeded, running (runtime ended is not succeeded)
			rows                  int
		}{
			{"both complete", false, false, 1, "ready", [3]int{1, 1, 1}, 4},
			{"team failure", true, false, 1, "partial", [3]int{0, 0, 1}, 2},
			{"runtime failure", false, true, 1, "partial", [3]int{1, 1, 0}, 2},
			{"both fail", true, true, 1, "unavailable", [3]int{}, 0},
			{"successful empty", false, false, 99, "ready", [3]int{}, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for table, down := range map[string]bool{"team_tasks": tc.teamDown, "gateway_runtime_tasks": tc.runtimeDown} {
					if down {
						exec("RENAME TABLE " + table + " TO " + table + "_offline")
						t.Cleanup(func() { exec("RENAME TABLE " + table + "_offline TO " + table) })
					}
				}
				view, e := store.Dashboard(context.Background(), tc.user, "7d", now)
				if e != nil {
					t.Fatal(e)
				}
				// Round-trip the actual frontend DTO: unavailable masks counters;
				// partial exposes retained known counts and its coverage note.
				raw, e := json.Marshal(view)
				if e != nil {
					t.Fatal(e)
				}
				var dto struct {
					Tasks    []struct{ Source, Status string }
					Coverage struct{ Tasks Coverage }
				}
				if e = json.Unmarshal(raw, &dto); e != nil {
					t.Fatal(e)
				}
				counts := [3]int{}
				for _, task := range dto.Tasks {
					switch task.Status {
					case "failed":
						counts[0]++
					case "succeeded":
						counts[1]++
					case "running":
						counts[2]++
					}
					if (tc.teamDown && task.Source == "team") || (tc.runtimeDown && task.Source == "runtime") {
						t.Errorf("failed source invented task: %+v", task)
					}
				}
				if dto.Coverage.Tasks.Status != tc.coverage || len(dto.Tasks) != tc.rows || counts != tc.counts {
					t.Errorf("frontend task status/count contract: coverage=%+v counts=%v rows=%d; want %s %v %d", dto.Coverage.Tasks, counts, len(dto.Tasks), tc.coverage, tc.counts, tc.rows)
				}
				if tc.coverage != "ready" && dto.Coverage.Tasks.Note == "" {
					t.Error("failed source lacks explanation")
				}
			})
		}
	})

	t.Run("terminal team events survive physical member instance deletion", func(t *testing.T) {
		exec("INSERT INTO team_members VALUES(12,12,2,2,'idle',?,'idle')", recent)
		exec(`INSERT INTO team_tasks VALUES(13,12,12,'succeeded','{"title":"Other owner"}',NULL,NULL,?,?,?)`, recent, recent, recent)
		exec("DELETE FROM instances WHERE id=1")
		memberRow, e := sess.SQL().QueryRow("SELECT instance_id IS NULL FROM team_members WHERE id=10")
		if e != nil {
			t.Fatal(e)
		}
		var detached bool
		if e = memberRow.Scan(&detached); e != nil || !detached {
			t.Fatal("production FK did not null member instance", e)
		}
		history, historyErr := store.Dashboard(context.Background(), 1, "7d", now)
		if historyErr != nil {
			t.Fatal(historyErr)
		}
		if history.Activity[364].Tasks != 2 || history.Coverage.Activity.Status != "partial" {
			t.Errorf("attributable terminal tasks disappeared: %+v %+v", history.Activity[364], history.Coverage.Activity)
		}
		exec("INSERT INTO instances VALUES(1,1,NULL,'Agent','openclaw','lite','running',1,2,5,2,?,?)", old, recent)
		exec("UPDATE team_members SET instance_id=1 WHERE id=10")
	})
	t.Run("long approved task JSON fields and native result aliases", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"title": "Visible title", "messages": strings.Repeat("private-conversation-sentinel", 600), "access_token": "secret-sentinel"})
		for _, alias := range []string{"summary", "resultMarkdown", "result_markdown", "result", "answer", "text", "message"} {
			result, _ := json.Marshal(map[string]any{alias: "Visible result", "body": strings.Repeat("private-result-sentinel", 600), "access_token": "secret-sentinel"})
			exec("UPDATE team_tasks SET payload_json=?,result_json=? WHERE id=11", string(payload), string(result))
			view := emptyDashboard("7d", now, d.Window)
			store.tasks(context.Background(), 1, now, view)
			found := false
			for _, task := range view.Tasks {
				if task.Source == "team" && task.ID == "11" {
					found = true
					if task.Title != "Visible title" || task.ResultExcerpt != "Visible result" {
						t.Errorf("%s lost valid long JSON fields: title=%q result=%q", alias, task.Title, task.ResultExcerpt)
					}
				}
			}
			if !found || view.Coverage.Tasks.Status != "ready" {
				t.Errorf("task extraction source unavailable: %+v", view.Coverage.Tasks)
			}
		}
		payload, _ = json.Marshal(map[string]any{"title": strings.Repeat("文", 200), "messages": []string{"private-conversation-sentinel"}})
		result, _ := json.Marshal(map[string]any{"summary": "", "result": map[string]string{"access_token": "secret-sentinel"}, "answer": strings.Repeat("答", 300)})
		exec("UPDATE team_tasks SET payload_json=?,result_json=? WHERE id=11", string(payload), string(result))
		limited := emptyDashboard("7d", now, d.Window)
		store.tasks(context.Background(), 1, now, limited)
		for _, task := range limited.Tasks {
			if task.Source == "team" && task.ID == "11" && (task.Title != strings.Repeat("文", 160) || task.ResultExcerpt != strings.Repeat("答", 240)) {
				t.Errorf("scalar bound or empty/nonstring fallback failed: title length=%d result length=%d", len([]rune(task.Title)), len([]rune(task.ResultExcerpt)))
			}
		}
		// Nonstring fields, malformed JSON and unrelated private fields never become excerpts.
		exec(`UPDATE team_tasks SET payload_json='invalid-json',result_json='{"summary":{"token":"secret-sentinel"},"resultMarkdown":["private-result-sentinel"],"access_token":"secret-sentinel"}' WHERE id=11`)
		view := emptyDashboard("7d", now, d.Window)
		store.tasks(context.Background(), 1, now, view)
		for _, task := range view.Tasks {
			if task.Source == "team" && task.ID == "11" && (task.Title != "团队任务" || task.ResultExcerpt != "") {
				t.Errorf("invalid/private fields exposed: %+v", task)
			}
		}
		exec(`UPDATE team_tasks SET payload_json='{"title":"Done"}',result_json='{"summary":"ok"}' WHERE id=11`)
	})
	t.Run("retention survives pass timeout and respects lifecycle cancellation", func(t *testing.T) {
		// The successful-empty Dashboard(99) case above recorded a current sample.
		// Isolate this retention fixture before asserting its expired row is gone.
		exec("DELETE FROM workspace_dashboard_observations WHERE user_id=99")
		expired := now.AddDate(0, 0, -401)
		exec("INSERT INTO workspace_dashboard_observations VALUES(99,?,?, '{}')", expired, expired)
		timedStore := NewStore(sess)
		timedStore.SetCapacityReader(func(ctx context.Context) (map[string]int, error) { <-ctx.Done(); return nil, ctx.Err() })
		timedStore.collect(context.Background(), now)
		countRows := func() int {
			r, e := sess.SQL().QueryRow("SELECT COUNT(*) FROM workspace_dashboard_observations WHERE user_id=99")
			if e != nil {
				t.Fatal(e)
			}
			var count int
			if e = r.Scan(&count); e != nil {
				t.Fatal(e)
			}
			return count
		}
		if countRows() != 0 {
			t.Error("expired observations survived exhausted collection budget")
		}
		exec("DELETE FROM workspace_dashboard_observations WHERE user_id=99")
		exec("INSERT INTO workspace_dashboard_observations VALUES(99,?,?, '{}')", expired, expired)
		lifecycle, cancel := context.WithCancel(context.Background())
		defer cancel()
		timedStore.SetCapacityReader(func(ctx context.Context) (map[string]int, error) { cancel(); return nil, ctx.Err() })
		started := time.Now()
		timedStore.collect(lifecycle, now)
		if time.Since(started) > time.Second || countRows() != 1 {
			t.Error("cleanup ignored lifecycle cancellation")
		}
	})
	exec("DROP TABLE cost_records")
	d, err = store.Dashboard(context.Background(), 1, "7d", now)
	if err != nil || d.Usage.TotalTokens == nil || *d.Usage.TotalTokens != 60 || d.Coverage.Costs.Status != "unavailable" {
		t.Fatal("cost source poisoned usage", d, err)
	}
	exec("DROP TABLE gateway_runtime_tasks")
	degraded, degradedErr := store.Dashboard(context.Background(), 1, "7d", now)
	if degradedErr != nil || degraded.InstancesTotal != 3 || degraded.Coverage.Instances.Status != "partial" || degraded.StateCounts["busy"] != 0 {
		t.Fatal("task telemetry failure poisoned core instances", degraded, degradedErr)
	}
	exec("DROP TABLE team_members")
	exec("DROP TABLE instances")
	if _, err = store.Dashboard(context.Background(), 1, "7d", now); err == nil {
		t.Fatal("core failure accepted")
	}
}
