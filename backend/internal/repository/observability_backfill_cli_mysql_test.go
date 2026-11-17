package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/systembackup"
)

func TestObservationBackfillCLIGateAndPreflight(t *testing.T) {
	binary := os.Getenv("CM_OBSERVATION_BACKFILL_BIN")
	if binary == "" {
		t.Skip("compiled observation-backfill CLI required")
	}
	s := auditFullFixture(t)
	ctx := context.Background()
	row, _ := s.SQL().QueryRow("SELECT DATABASE()")
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`INSERT INTO model_invocations(id,trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,created_at)VALUES(1,'cli-backfill','cli-request','auto','model','openai','llm','completed',15,UTC_TIMESTAMP()-INTERVAL 1 HOUR)`, `UPDATE observation_summary_state SET target_id=1,ready=FALSE WHERE id=1`, `UPDATE observation_audit_backfill SET target_id=1 WHERE source IN('model_invocations','observation_invocation_summaries')`} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := (systembackup.Gate{DB: s.Driver().(*sql.DB)}).EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "configs"), 0700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("database:\n  host: 127.0.0.1\n  port: %s\n  user: root\n  password: %q\n  database: %s\n", os.Getenv("TEST_DATABASE_MYSQL_PORT"), os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), name)
	if err := os.WriteFile(filepath.Join(dir, "configs", "dev.yaml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	dsnFile := filepath.Join(dir, "control.dsn")
	dsn := fmt.Sprintf("root:%s@tcp(127.0.0.1:%s)/%s?parseTime=true", os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), os.Getenv("TEST_DATABASE_MYSQL_PORT"), name)
	if err := os.WriteFile(dsnFile, []byte(dsn), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) ([]byte, error) {
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(runCtx, binary, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SYSTEM_BACKUP_SQL_GATE_SCOPE=installation", "SYSTEM_BACKUP_CONTROL_DSN_FILE="+dsnFile)
		return cmd.CombinedOutput()
	}
	if out, err := run("--check"); err == nil || !strings.Contains(string(out), "preflight failed") {
		t.Fatalf("preflight did not identify rebuilding: %v %s", err, out)
	}
	if _, err := s.SQL().Exec(`UPDATE system_backup_gate_admissions SET owner='cli-test',close_until=UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE WHERE scope='installation'`); err != nil {
		t.Fatal(err)
	}
	if out, err := run("--timeout=5s"); err == nil {
		t.Fatalf("backfill bypassed backup gate: %s", out)
	} else {
		t.Logf("GATE_REFUSAL %s", out)
	}
	row, _ = s.SQL().QueryRow("SELECT COUNT(*) FROM observation_invocation_summaries")
	var n int
	if err := row.Scan(&n); err != nil || n != 0 {
		t.Fatalf("closed gate allowed facts: %d %v", n, err)
	}
	if _, err := s.SQL().Exec(`UPDATE system_backup_gate_admissions SET owner=NULL,close_until=NULL WHERE scope='installation'`); err != nil {
		t.Fatal(err)
	}
	if out, err := run("--timeout=10s"); err != nil {
		t.Fatalf("backfill failed: %v %s", err, out)
	} else {
		t.Logf("BACKFILL_READY %s", out)
	}
	if out, err := run("--check"); err != nil {
		t.Fatalf("ready preflight failed: %v %s", err, out)
	}
	held, err := s.Driver().(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	for _, q := range []string{`INSERT INTO model_invocations(trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,created_at)VALUES('cli-held-low','cli-held-low','auto','model','openai','llm','completed',0,UTC_TIMESTAMP()-INTERVAL 1 HOUR)`, `INSERT INTO cost_records(trace_id,provider_type,model_name,currency,estimated_cost,recorded_at)VALUES('cli-held-low','openai','model','USD',2,UTC_TIMESTAMP())`} {
		if _, err = held.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{`INSERT INTO model_invocations(trace_id,request_id,requested_model,actual_provider_model,provider_type,traffic_class,status,total_tokens,created_at)VALUES('cli-high','cli-high','auto','model','openai','llm','completed',0,UTC_TIMESTAMP()-INTERVAL 1 HOUR)`, `INSERT INTO cost_records(trace_id,provider_type,model_name,currency,estimated_cost,recorded_at)VALUES('cli-high','openai','model','USD',2,UTC_TIMESTAMP())`} {
		if _, err = s.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := run("--timeout=10s"); err != nil {
		t.Fatalf("high ID catchup: %v %s", err, out)
	}
	if err = held.Commit(); err != nil {
		t.Fatal(err)
	}
	if out, err := run("--check"); err == nil {
		t.Fatalf("restarted CLI ignored newly committed low IDs: %s", out)
	}
	if out, err := run("--timeout=10s"); err != nil {
		t.Fatalf("restarted CLI low-ID catchup: %v %s", err, out)
	}
	for _, table := range []string{"observation_invocation_summaries", "observation_cost_facts"} {
		row, err := s.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE trace_id='cli-held-low'")
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err = row.Scan(&n); err != nil || n != 1 {
			t.Fatalf("CLI omitted low ID %s %d %v", table, n, err)
		}
	}
	p, err := NewModelInvocationRepository(s).(*modelInvocationRepository).ListAuditReportPage(ctx, AuditReportQuery{Limit: 20})
	if err != nil || p.Total != 3 {
		t.Fatalf("CLI serving cutover: %+v %v", p, err)
	}
}
