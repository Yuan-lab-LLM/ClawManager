package repository

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

// databaseReportingMySQL only touches a newly created database on an explicitly enabled localhost fixture.
func databaseReportingMySQL(t *testing.T) db.Session {
	t.Helper()
	port := os.Getenv("TEST_DATABASE_MYSQL_PORT")
	if port == "" {
		t.Skip("TEST_DATABASE_MYSQL_PORT enables disposable localhost MySQL tests")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		t.Fatal("valid localhost fixture port required")
	}
	u := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Password: os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	row, err := root.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = 'cm_database_test_marker'")
	if err != nil {
		root.Close()
		t.Fatal(err)
	}
	var marker int
	if err = row.Scan(&marker); err != nil || marker != 1 {
		root.Close()
		t.Fatalf("disposable fixture marker required: %d %v", marker, err)
	}
	name := fmt.Sprintf("cm_database_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	u.Database = name
	sess, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess
}

func TestCostReportingCountsEachEligibleRecordOnce(t *testing.T) {
	s := databaseReportingMySQL(t)
	for _, q := range []string{
		`CREATE TABLE model_invocations(id INT PRIMARY KEY,trace_id VARCHAR(100),instance_id INT,session_id VARCHAR(100),status VARCHAR(50),created_at DATETIME(6))`,
		`CREATE TABLE cost_records(id INT PRIMARY KEY,trace_id VARCHAR(100),instance_id INT,session_id VARCHAR(100),invocation_id INT NULL,prompt_tokens INT,completion_tokens INT,total_tokens INT,estimated_cost DECIMAL(18,8),currency VARCHAR(16))`,
		`INSERT INTO model_invocations VALUES (1,'retry',9,'main','failed','2026-11-17 12:00:00'),(2,'retry',9,'main','completed','2026-11-17 12:01:00'),(3,'retry',9,'main','blocked','2026-11-17 12:02:00'),(4,'retry',9,'other','completed','2026-11-17 12:00:00'),(5,'retry',10,'main','completed','2026-11-17 12:00:00'),(6,'retry',9,'main','completed','2026-11-16 12:00:00'),(7,'only-other-session',9,'other','completed','2026-11-17 12:00:00'),(8,'until-boundary',9,'main','completed','2026-11-18 00:00:00')`,
		`INSERT INTO cost_records VALUES (1,'retry',9,'main',1,10,1,11,1,'USD'),(2,'retry',9,'main',2,20,2,22,2,'USD'),(3,'retry',9,'main',NULL,30,3,33,3,'USD'),(4,'retry',9,'main',3,400,40,440,40,'USD'),(5,'retry',9,'main',999,500,50,550,50,'USD'),(6,'retry',9,'main',4,600,60,660,60,'USD'),(7,'retry',9,'main',5,700,70,770,70,'USD'),(8,'retry',9,'main',6,800,80,880,80,'USD'),(9,'only-other-session',9,'main',NULL,900,90,990,90,'USD'),(10,'until-boundary',9,'main',8,1000,100,1100,100,'USD')`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	rows, err := NewCostRecordRepository(s).AggregateCostByInstanceSession(9, SessionUsageFilter{Since: &since, Until: &until})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != "main" || rows[0].EstimatedCost != 6 || rows[0].PromptTokens != 60 || rows[0].CompletionTokens != 6 || rows[0].TotalTokens != 66 {
		t.Fatalf("eligible fees must count once with exact association and scoped legacy fallback: %+v", rows)
	}
}

func TestSessionReportingInvocationProjectionOmitsBodies(t *testing.T) {
	s := databaseReportingMySQL(t)
	_, err := s.SQL().Exec(`CREATE TABLE model_invocations(id INT PRIMARY KEY,trace_id VARCHAR(100),session_id VARCHAR(100),request_id VARCHAR(100),user_id INT,instance_id INT,instance_mode VARCHAR(16),runtime_type VARCHAR(32),gateway_id VARCHAR(128),runtime_pod_id BIGINT,gateway_key_id INT,model_id INT,provider_type VARCHAR(100),requested_model VARCHAR(255),actual_provider_model VARCHAR(255),traffic_class VARCHAR(50),request_payload LONGTEXT,response_payload LONGTEXT,prompt_tokens INT,completion_tokens INT,total_tokens INT,cached_tokens INT,reasoning_tokens INT,latency_ms INT,ttft_ms INT,generation_ms INT,tokens_per_second DOUBLE,attempt_no INT,is_streaming BOOLEAN,status VARCHAR(50),error_message TEXT,created_at DATETIME(6),completed_at DATETIME(6))`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SQL().Exec(`INSERT INTO model_invocations(id,trace_id,session_id,request_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,request_payload,response_payload,prompt_tokens,completion_tokens,total_tokens,attempt_no,is_streaming,status,created_at) VALUES (1,'trace','main','request',9,'openai','a','b','chat','request body','response body',1,2,3,1,false,'completed','2026-11-17 12:00:00')`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewModelInvocationRepository(s)
	items, err := repo.ListRecentByInstanceSession(9, "main", 20, SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TotalTokens != 3 || items[0].RequestPayload != nil || items[0].ResponsePayload != nil {
		t.Fatalf("reporting must omit bodies while retaining metadata: %+v", items)
	}
	aggregateRepo, ok := repo.(interface {
		AggregateModelsByInstanceSession(int, string, SessionUsageFilter) ([]models.ModelInvocation, error)
	})
	if !ok {
		t.Fatal("SQL model breakdown aggregation capability missing")
	}
	for id := 2; id <= 25; id++ {
		if _, err := s.SQL().Exec(`INSERT INTO model_invocations(id,trace_id,session_id,request_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,prompt_tokens,completion_tokens,total_tokens,attempt_no,is_streaming,status,created_at) SELECT ?,trace_id,session_id,request_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,prompt_tokens,completion_tokens,total_tokens,attempt_no,is_streaming,status,created_at FROM model_invocations WHERE id=1`, id); err != nil {
			t.Fatal(err)
		}
	}
	aggregates, err := aggregateRepo.AggregateModelsByInstanceSession(9, "main", SessionUsageFilter{})
	if err != nil || len(aggregates) != 1 || aggregates[0].ActualProviderModel != "b" || aggregates[0].TotalTokens != 75 {
		t.Fatalf("full model aggregation: %+v %v", aggregates, err)
	}
	detail, err := repo.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestPayload == nil || *detail.RequestPayload != "request body" || detail.ResponsePayload == nil || *detail.ResponsePayload != "response body" {
		t.Fatalf("true invocation detail lost bodies: %+v", detail)
	}
}

func TestCostReportingModelAggregationUsesEligibleCostIdentity(t *testing.T) {
	s := databaseReportingMySQL(t)
	repo := NewCostRecordRepository(s)
	aggregateRepo, ok := repo.(interface {
		AggregateModelCostByInstanceSession(int, string, SessionUsageFilter) ([]models.CostRecord, error)
	})
	if !ok {
		t.Fatal("SQL cost model aggregation capability missing")
	}
	for _, q := range []string{
		`CREATE TABLE model_invocations(id INT PRIMARY KEY,trace_id VARCHAR(100),instance_id INT,session_id VARCHAR(100),status VARCHAR(50),created_at DATETIME(6))`,
		`CREATE TABLE cost_records(id INT PRIMARY KEY,trace_id VARCHAR(100),instance_id INT,session_id VARCHAR(100),invocation_id INT NULL,model_name VARCHAR(255),estimated_cost DECIMAL(18,8),currency VARCHAR(16))`,
		`INSERT INTO model_invocations VALUES(1,'retry',9,'main','failed','2026-11-17 12:00:00'),(2,'retry',9,'main','completed','2026-11-17 12:00:00'),(3,'retry',9,'main','blocked','2026-11-17 12:00:00')`,
		`INSERT INTO cost_records VALUES(1,'retry',9,'main',1,'billed-a',1,'USD'),(2,'retry',9,'main',2,'billed-b',2,'USD'),(3,'retry',9,'main',NULL,'billed-b',3,'USD'),(4,'retry',9,'main',3,'blocked',100,'EUR')`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := aggregateRepo.AggregateModelCostByInstanceSession(9, "main", SessionUsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]float64{}
	for _, row := range rows {
		totals[row.ModelName] = row.EstimatedCost
	}
	if len(totals) != 2 || totals["billed-a"] != 1 || totals["billed-b"] != 5 {
		t.Fatalf("wrong full-session billed model totals: %+v", totals)
	}
}

func TestCostReportingRejectsMixedCurrencies(t *testing.T) {
	for _, tc := range []struct {
		name, secondModel, secondSession, firstCurrency, secondCurrency, wantCurrency string
		mixed                                                                         bool
	}{
		{"same-model", "billed-a", "main", "USD", "EUR", "", true},
		{"different-models", "billed-b", "main", "USD", "EUR", "", true},
		{"different-sessions", "billed-b", "other", "USD", "EUR", "", true},
		{"normalized-usd", "billed-b", "main", " usd ", "", "USD", false},
		{"single-non-usd", "billed-b", "main", " cny ", "CNY", "CNY", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := databaseReportingMySQL(t)
			for _, q := range []string{
				`CREATE TABLE model_invocations(id INT PRIMARY KEY,trace_id VARCHAR(100),instance_id INT,session_id VARCHAR(100),status VARCHAR(50),created_at DATETIME(6))`,
				`CREATE TABLE cost_records(id INT PRIMARY KEY,trace_id VARCHAR(100),instance_id INT,session_id VARCHAR(100),invocation_id INT NULL,model_name VARCHAR(255),prompt_tokens INT,completion_tokens INT,total_tokens INT,estimated_cost DECIMAL(18,8),currency VARCHAR(16))`,
				`INSERT INTO model_invocations VALUES(1,'first',9,'main','completed','2026-11-17 12:00:00')`,
			} {
				if _, err := s.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.SQL().Exec(`INSERT INTO model_invocations VALUES(2,'second',9,?,'completed','2026-11-17 12:00:00')`, tc.secondSession); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SQL().Exec(`INSERT INTO cost_records VALUES(1,'first',9,'main',1,'billed-a',1,2,3,1,?),(2,'second',9,?,2,?,4,5,9,10,?)`, tc.firstCurrency, tc.secondSession, tc.secondModel, tc.secondCurrency); err != nil {
				t.Fatal(err)
			}
			repo := NewCostRecordRepository(s)
			rows, err := repo.AggregateCostByInstanceSession(9, SessionUsageFilter{})
			if tc.mixed {
				if !errors.Is(err, ErrMixedCostCurrencies) || rows != nil {
					t.Errorf("mixed session scope must fail without partial totals: %+v %v", rows, err)
				}
			} else if err != nil || len(rows) != 1 || rows[0].EstimatedCost != 11 || rows[0].Currency != tc.wantCurrency {
				t.Errorf("homogeneous normalized total: %+v %v", rows, err)
			}
			modelRepo := repo.(interface {
				AggregateModelCostByInstanceSession(int, string, SessionUsageFilter) ([]models.CostRecord, error)
			})
			models, err := modelRepo.AggregateModelCostByInstanceSession(9, "main", SessionUsageFilter{})
			if tc.mixed && tc.secondSession == "main" {
				if !errors.Is(err, ErrMixedCostCurrencies) || models != nil {
					t.Errorf("mixed model scope must fail without partial totals: %+v %v", models, err)
				}
			} else {
				wantTotal, wantCurrency := 11.0, tc.wantCurrency
				if tc.secondSession != "main" {
					wantTotal, wantCurrency = 1, "USD"
				}
				total := 0.0
				for _, row := range models {
					total += row.EstimatedCost
					if row.Currency != wantCurrency {
						t.Errorf("model currency lost: %+v", row)
					}
				}
				if err != nil || total != wantTotal {
					t.Errorf("homogeneous model totals: %+v %v", models, err)
				}
			}
			until := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
			empty, err := repo.AggregateCostByInstanceSession(9, SessionUsageFilter{Until: &until})
			if err != nil || len(empty) != 0 {
				t.Errorf("empty window: %+v %v", empty, err)
			}
			emptyModels, err := modelRepo.AggregateModelCostByInstanceSession(9, "main", SessionUsageFilter{Until: &until})
			if err != nil || len(emptyModels) != 0 {
				t.Errorf("empty model window: %+v %v", emptyModels, err)
			}
		})
	}
}
