package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"clawreef/internal/systembackup"
)

// Opt-in synthetic worst-case trace cardinality: 10,000 instances, one distinct
// trace per instance/day for 90 days. This is not a measured customer workload.
func TestAuditCapacityActualSourceBackfillAndQueries(t *testing.T) {
	if os.Getenv("CM_AUDIT_CAPACITY_TEST") != "1" {
		t.Skip("900000-source-row capacity fixture opt-in")
	}
	binary := os.Getenv("CM_OBSERVATION_BACKFILL_BIN")
	if binary == "" {
		t.Fatal("compiled observation-backfill CLI required")
	}
	s := auditFullFixture(t)
	ctx := context.Background()
	execute := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := s.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	row, _ := s.SQL().QueryRow("SELECT DATABASE()")
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatal(err)
	}
	execute(`INSERT IGNORE INTO users(id,username,email,password_hash) VALUES(1,'capacity-auditor','capacity-auditor@example.test','fixture')`)
	execute(`CREATE TABLE audit_capacity_digits(n INT PRIMARY KEY)`)
	execute(`INSERT INTO audit_capacity_digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`)
	execute(`INSERT INTO instances(id,user_id,name,type,status,cpu_cores,memory_gb,disk_gb,os_type,os_version) SELECT 1+a.n+10*b.n+100*c.n+1000*d.n,1,CONCAT('audit-capacity-',1+a.n+10*b.n+100*c.n+1000*d.n),'openclaw','running',1,1,1,'linux','fixture' FROM audit_capacity_digits a CROSS JOIN audit_capacity_digits b CROSS JOIN audit_capacity_digits c CROSS JOIN audit_capacity_digits d`)
	since := time.Now().UTC().Truncate(24 * time.Hour).Add(-90 * 24 * time.Hour)
	until := since.Add(90 * 24 * time.Hour)
	seedStart := time.Now()
	execute(`INSERT INTO model_invocations(id,trace_id,session_id,request_id,user_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,status,prompt_tokens,completion_tokens,total_tokens,created_at) SELECT (i.id-1)*90+days.n+1,CONCAT('audit-',i.id,'-',days.n),'main',CONCAT('req-',i.id,'-',days.n),1,i.id,'openai','auto','capacity-model','llm','completed',10,5,15,DATE_ADD(?,INTERVAL days.n DAY) FROM instances i CROSS JOIN (SELECT a.n+10*b.n n FROM audit_capacity_digits a CROSS JOIN audit_capacity_digits b WHERE a.n+10*b.n<90) days`, since)
	t.Logf("CAPACITY_SOURCE database=%s instances=10000 days=90 rows=900000 distinct_traces=900000 seed_elapsed=%s", name, time.Since(seedStart))
	execute(`UPDATE observation_summary_state SET target_id=900000,ready=FALSE WHERE id=1`)
	execute(`UPDATE observation_audit_backfill SET target_id=900000 WHERE source IN('model_invocations','observation_invocation_summaries')`)
	pool := s.Driver().(*sql.DB)
	if err := (systembackup.Gate{DB: pool}).EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "configs"), 0700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("database:\n  host: 127.0.0.1\n  port: %s\n  user: root\n  password: %q\n  database: %s\n  maxOpenConns: 4\n  maxIdleConns: 2\n", os.Getenv("TEST_DATABASE_MYSQL_PORT"), os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), name)
	if err := os.WriteFile(filepath.Join(dir, "configs", "dev.yaml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	dsnFile := filepath.Join(dir, "control.dsn")
	dsn := fmt.Sprintf("root:%s@tcp(127.0.0.1:%s)/%s?parseTime=true", os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), os.Getenv("TEST_DATABASE_MYSQL_PORT"), name)
	if err := os.WriteFile(dsnFile, []byte(dsn), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(runCtx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(runCtx, binary, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SYSTEM_BACKUP_SQL_GATE_SCOPE=installation", "SYSTEM_BACKUP_CONTROL_DSN_FILE="+dsnFile)
		return cmd.CombinedOutput()
	}
	if out, err := run(ctx, "--check"); err == nil {
		t.Fatalf("preflight accepted incomplete summary: %s", out)
	}
	execute(`UPDATE system_backup_gate_admissions SET owner='audit-fixture-fence',close_until=UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE WHERE scope='installation'`)
	if out, err := run(ctx, "--timeout=10s"); err == nil {
		t.Fatalf("backup gate allowed backfill: %s", out)
	} else {
		t.Logf("BACKUP_GATE_REFUSAL %s", out)
	}
	row, _ = s.SQL().QueryRow("SELECT COUNT(*) FROM observation_audit_trace_reports")
	var count int
	if err := row.Scan(&count); err != nil || count != 0 {
		t.Fatalf("closed gate mutated summaries: %d %v", count, err)
	}
	execute(`UPDATE system_backup_gate_admissions SET owner=NULL,close_until=NULL WHERE scope='installation'`)
	begin := time.Now()
	interrupted, cancel := context.WithTimeout(ctx, 2*time.Second)
	out, err := run(interrupted, "--timeout=30m")
	cancel()
	if err == nil {
		t.Fatal("interruption did not interrupt")
	}
	t.Logf("INTERRUPTED_BACKFILL %s", out)
	row, _ = s.SQL().QueryRow("SELECT COUNT(*) FROM observation_invocation_summaries")
	if err = row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 || count >= 900000 {
		t.Fatalf("interruption boundary not exercised: %d", count)
	}
	t.Logf("INTERRUPTION_COMMITTED_FACTS %d", count)
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	out, err = run(runCtx, "--timeout=29m")
	cancel()
	t.Logf("BACKFILL_CLI_PROGRESS %s", out)
	if err != nil {
		t.Fatalf("backfill CLI: %v", err)
	}
	t.Logf("MEASURED actual_source_rows=900000 bounded_batch=500 includes_crash_resume=true backfill_elapsed=%s", time.Since(begin))
	if out, err = run(ctx, "--check"); err != nil {
		t.Fatalf("preflight after complete: %v %s", err, out)
	}
	row, _ = s.SQL().QueryRow("SELECT COUNT(*),SUM(total_tokens) FROM observation_audit_trace_reports WHERE id>0")
	var totalTokens int64
	if err = row.Scan(&count, &totalTokens); err != nil || count != 900000 || totalTokens != 13500000 {
		t.Fatalf("exact backfill totals count=%d tokens=%d err=%v", count, totalTokens, err)
	}
	reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
	// Equivalent old full-history grouping for this invocation-only fixture.
	// Keep a finite budget and report timeouts as timeouts, never as passes.
	for i := 0; i < 3; i++ {
		baselineCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		at := time.Now()
		row, e := s.SQL().QueryRowContext(baselineCtx, `WITH invocation_groups AS (SELECT trace_id,MIN(id) first_id,MAX(id) last_id,MIN(created_at) created_at,SUM(total_tokens) total_tokens FROM model_invocations WHERE trace_id<>'' GROUP BY trace_id) SELECT COUNT(*),SUM(g.total_tokens) FROM invocation_groups g JOIN model_invocations m ON m.id=g.last_id JOIN model_invocations f ON f.id=g.first_id LEFT JOIN users u ON u.id=m.user_id WHERE g.created_at>=? AND g.created_at<?`, since, until)
		var count, tokens int64
		if e == nil {
			e = row.Scan(&count, &tokens)
		}
		elapsed := time.Since(at)
		cancel()
		t.Logf("LEGACY_BASELINE full_history_CTE sample=%d elapsed=%s count=%d tokens=%d error=%v", i, elapsed, count, tokens, e)
		if e == nil && (count != 900000 || tokens != 13500000) {
			t.Fatalf("baseline mismatch %d %d", count, tokens)
		}
	}
	for _, scenario := range []struct {
		name, search string
		days         int
		want         int
	}{{"all90", "", 90, 900000}, {"default30-equivalent", "", 30, 300000}, {"search30", "capacity-model", 30, 300000}, {"search90", "capacity-model", 90, 900000}} {
		start := until.Add(-time.Duration(scenario.days) * 24 * time.Hour)
		samples := []time.Duration{}
		var next string
		for i := 0; i < 20; i++ {
			at := time.Now()
			p, e := reporter.ListAuditReportPage(ctx, AuditReportQuery{Limit: 20, Since: &start, Until: &until, Search: scenario.search})
			samples = append(samples, time.Since(at))
			if e != nil || p.Total != scenario.want || len(p.Items) != 20 || p.NextCursor == "" {
				t.Fatalf("capacity %s %+v %v", scenario.name, p, e)
			}
			next = p.NextCursor
		}
		p, e := reporter.ListAuditReportPage(ctx, AuditReportQuery{Limit: 20, Cursor: next, Search: scenario.search})
		if e != nil || p.Total != scenario.want || len(p.Items) != 20 {
			t.Fatalf("next %s %+v %v", scenario.name, p, e)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("MEASURED scenario=%s instances=10000 source_rows=900000 trace_rows=900000 samples=20 P50=%s P95=%s MAX=%s", scenario.name, samples[9], samples[18], samples[19])
		budget := 2 * time.Second
		if scenario.days == 30 {
			budget = 500 * time.Millisecond
		}
		if samples[18] > budget {
			t.Errorf("%s P95 %s exceeds fixed %s target", scenario.name, samples[18], budget)
		}
	}
	for _, query := range []string{`SELECT COUNT(*) FROM observation_audit_trace_reports r WHERE r.id>0 AND r.sort_at>=? AND r.sort_at<? AND r.sequence_id<=(SELECT MAX(sequence_id) FROM observation_audit_trace_reports)`, `SELECT r.id,r.trace_id,r.request_id,r.status,r.total_tokens FROM observation_audit_trace_reports r WHERE r.id>0 AND r.sort_at>=? AND r.sort_at<? AND r.sequence_id<=(SELECT MAX(sequence_id) FROM observation_audit_trace_reports) ORDER BY r.sort_at DESC,r.trace_id DESC LIMIT 21`} {
		row, e := s.SQL().QueryRow("EXPLAIN FORMAT=JSON "+query, since, until)
		if e != nil {
			t.Fatal(e)
		}
		var plan string
		if e = row.Scan(&plan); e != nil {
			t.Fatal(e)
		}
		t.Logf("EXPLAIN_ACTUAL %s %s", query, strings.ReplaceAll(plan, "\n", " "))
	}
}
