package repository

import (
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func reportingFixture(t *testing.T) db.Session {
	t.Helper()
	s := databaseReportingMySQL(t)
	schema, err := os.ReadFile("../db/migrations/028_add_ai_gateway_runtime_attribution.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(strings.Split(string(schema), "SET @column_exists")[0], ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := s.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, statement := range []string{
		`ALTER TABLE model_invocations ADD COLUMN ttft_ms INT NULL, ADD COLUMN generation_ms INT NULL, ADD COLUMN tokens_per_second DOUBLE NULL, ADD COLUMN attempt_no INT NOT NULL DEFAULT 1`,
		`CREATE TABLE users(id INT PRIMARY KEY,username VARCHAR(100) NOT NULL)`,
		`CREATE TABLE instances(id INT PRIMARY KEY,user_id INT,name VARCHAR(255),type VARCHAR(64),status VARCHAR(32))`,
		`CREATE TABLE chat_sessions(id INT PRIMARY KEY,instance_id INT,session_id VARCHAR(100),title VARCHAR(255),last_activity_at DATETIME,UNIQUE KEY(instance_id,session_id)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`INSERT INTO users VALUES(1,'operator'),(2,'other')`,
		`INSERT INTO instances VALUES(9,1,'primary','openclaw','running'),(10,2,'other','hermes','stopped'),(11,1,'unmanaged','ubuntu','running')`,
	} {
		if _, err := s.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func addReportingInvocation(t *testing.T, s db.Session, id, instance int, trace, session, model, status string, created time.Time) {
	t.Helper()
	_, err := s.SQL().Exec(`INSERT INTO model_invocations(id,trace_id,session_id,request_id,user_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,status,prompt_tokens,completion_tokens,total_tokens,created_at,request_payload,response_payload) VALUES(?,?,?, ?,1,?,'openai','auto',?,'llm',?,10,5,15,?,'large request body','large response body')`, id, trace, session, trace+"-request", instance, model, status, created)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuditSQLPagingIncludesHistoricalMatchesAndTraceTotals(t *testing.T) {
	s := reportingFixture(t)
	reporter, ok := NewModelInvocationRepository(s).(interface {
		ListAuditReport(int, int, string, string, string) ([]models.ModelInvocation, int, error)
	})
	if !ok {
		t.Fatal("SQL audit paging capability missing; legacy recent window loses historical matches")
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 1005; i++ {
		addReportingInvocation(t, s, i, 9, "new-"+time.Duration(i).String(), "main", "current-model", "completed", now.Add(time.Duration(i)*time.Second))
	}
	addReportingInvocation(t, s, 2000, 9, "historical-match", "main", "older-model", "failed", now.Add(-time.Hour))
	addReportingInvocation(t, s, 2001, 9, "historical-match", "main", "older-model", "completed", now.Add(-30*time.Minute))
	items, total, err := reporter.ListAuditReport(1, 20, "historical-match", "completed", "older-model")
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("historical match lost: items=%+v total=%d err=%v", items, total, err)
	}
	if items[0].TotalTokens != 30 || !items[0].CreatedAt.Equal(now.Add(-time.Hour)) || items[0].Status != "completed" || items[0].RequestPayload != nil || items[0].ResponsePayload != nil {
		t.Fatalf("trace aggregate or narrow projection incorrect: %+v", items[0])
	}
	all, total, err := reporter.ListAuditReport(51, 20, "", "", "")
	if err != nil || total != 1006 || len(all) != 6 {
		t.Fatalf("accurate deep page: %d %d %v", total, len(all), err)
	}
	_, total, err = reporter.ListAuditReport(1, 20, "operator", "completed", "")
	if err != nil || total != 1006 {
		t.Fatalf("username filter must apply before paging: %d %v", total, err)
	}
}

func TestAuditSQLPagingIncludesEventOnlyTracesAndStableOrder(t *testing.T) {
	s := reportingFixture(t)
	reporter, ok := NewModelInvocationRepository(s).(interface {
		ListAuditReport(int, int, string, string, string) ([]models.ModelInvocation, int, error)
	})
	if !ok {
		t.Fatal("SQL audit paging capability missing")
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	addReportingInvocation(t, s, 1, 9, "trace-a", "main", "model", "completed", now)
	addReportingInvocation(t, s, 2, 9, "trace-b", "main", "model", "completed", now)
	for _, statement := range []string{
		`INSERT INTO audit_events(trace_id,request_id,user_id,instance_id,event_type,traffic_class,severity,message,created_at) VALUES('event-only','r',1,9,'gateway.request.failed','llm','error','distinct failure','2026-11-17 11:00:00'),('trace-a','r',1,9,'gateway.request.failed','llm','error','old event','2026-11-17 11:00:00')`,
		`INSERT INTO cost_records(trace_id,user_id,instance_id,provider_type,model_name,currency,prompt_tokens,completion_tokens,total_tokens,estimated_cost) VALUES('event-only',1,9,'openai','fallback-model','USD',7,3,10,0.5)`,
	} {
		if _, err := s.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := reporter.ListAuditReport(1, 1, "", "", "")
	if err != nil || total != 3 || len(items) != 1 || items[0].TraceID != "trace-b" {
		t.Fatalf("stable first page: %+v %d %v", items, total, err)
	}
	items, total, err = reporter.ListAuditReport(1, 20, "distinct failure", "failed", "fallback-model")
	if err != nil || total != 1 || len(items) != 1 || items[0].TraceID != "event-only" || items[0].TotalTokens != 10 {
		t.Fatalf("event-only trace filtering: %+v %d %v", items, total, err)
	}
}

func TestReportingUsernameBatchReturnsOnlyRequestedUsers(t *testing.T) {
	s := reportingFixture(t)
	reader, ok := NewUserRepository(s).(interface {
		UsernamesByIDs([]int) (map[int]string, error)
	})
	if !ok {
		t.Fatal("bounded username batch capability missing")
	}
	names, err := reader.UsernamesByIDs([]int{1, 1, 99})
	if err != nil || len(names) != 1 || names[1] != "operator" {
		t.Fatalf("unexpected username projection: %+v %v", names, err)
	}
	names, err = reader.UsernamesByIDs(nil)
	if err != nil || len(names) != 0 {
		t.Fatalf("empty batch should be empty: %+v %v", names, err)
	}
}
