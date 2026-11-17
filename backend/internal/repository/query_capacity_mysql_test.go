package repository

import (
	"clawreef/internal/config"
	database "clawreef/internal/db"
	"context"
	"database/sql"
	"fmt"
	"github.com/upper/db/v4"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Explicit opt-in: synthetic capacity fixture, never a production benchmark.
// One invocation/active instance/day is the declared scenario, not a measured
// customer request rate. Payload samples measure this fixture's 1KiB/2KiB bodies.
func TestQueryCapacityTenThousandInstancesNinetyDays(t *testing.T) {
	if os.Getenv("CM_QUERY_CAPACITY_TEST") != "1" {
		t.Skip("capacity fixture opt-in")
	}
	raw := databaseReportingMySQL(t)
	row, e := raw.SQL().QueryRow("SELECT DATABASE()")
	if e != nil {
		t.Fatal(e)
	}
	var name string
	if e = row.Scan(&name); e != nil {
		t.Fatal(e)
	}
	port, _ := strconv.Atoi(os.Getenv("TEST_DATABASE_MYSQL_PORT"))
	s, e := database.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Password: os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), Database: name, MaxOpenConns: 4, MaxIdleConns: 4})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var peakHeap atomic.Uint64
	stopMemory := make(chan struct{})
	memoryDone := make(chan struct{})
	go func() {
		defer close(memoryDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMemory:
				return
			case <-ticker.C:
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				if mem.HeapAlloc > peakHeap.Load() {
					peakHeap.Store(mem.HeapAlloc)
				}
			}
		}
	}()
	defer func() {
		close(stopMemory)
		<-memoryDone
		t.Logf("MEASURED Go sampled peak heap bytes=%d (10ms interval, includes fixture creation)", peakHeap.Load())
		if driver, ok := s.Driver().(*sql.DB); ok {
			stats := driver.Stats()
			t.Logf("MEASURED SQL pool wait_count=%d wait_duration=%s max_open=%d", stats.WaitCount, stats.WaitDuration, stats.MaxOpenConnections)
		}
		if raw, e := os.ReadFile("/proc/self/status"); e == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "VmHWM:") {
					t.Log("MEASURED process " + line)
				}
			}
		}
	}()
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := s.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT IGNORE INTO users(id,username,email,password_hash) VALUES(1,'capacity','capacity@example.test','fixture')`)
	exec(`CREATE TABLE query_capacity_digits(n INT PRIMARY KEY)`)
	exec(`INSERT INTO query_capacity_digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`)
	exec(`INSERT INTO instances(id,user_id,name,type,status,cpu_cores,memory_gb,disk_gb,os_type,os_version) SELECT 1+a.n+10*b.n+100*c.n+1000*d.n,1,CONCAT('capacity-',1+a.n+10*b.n+100*c.n+1000*d.n),'openclaw','running',1,1,1,'linux','fixture' FROM query_capacity_digits a CROSS JOIN query_capacity_digits b CROSS JOIN query_capacity_digits c CROSS JOIN query_capacity_digits d`)
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(-90 * 24 * time.Hour)
	until := start.Add(90 * 24 * time.Hour)
	since := until.Add(-30 * 24 * time.Hour)
	prior := 0
	for _, active := range []int{1000, 3000, 10000} {
		exec(`INSERT INTO observation_invocation_summaries(id,instance_id,user_id,session_id,trace_id,request_id,requested_model,actual_provider_model,provider_type,status,prompt_tokens,completion_tokens,total_tokens,created_at) SELECT (i.id-1)*90+days.n+1,i.id,1,'main',CONCAT('trace-',i.id),CONCAT('req-',i.id,'-',days.n),'m','m','openai','completed',10,5,15,DATE_ADD(?,INTERVAL days.n DAY) FROM instances i CROSS JOIN (SELECT a.n+10*b.n n FROM query_capacity_digits a CROSS JOIN query_capacity_digits b WHERE a.n+10*b.n<90) days WHERE i.id>? AND i.id<=?`, start, prior, active)
		for _, scope := range []string{"session", "instance", "day", "trace"} {
			dimension := "''"
			if scope == "session" {
				dimension = "session_id"
			}
			if scope == "trace" {
				dimension = "trace_id"
			}
			exec(`INSERT INTO observation_daily_summaries SELECT ?,instance_id,`+dimension+`,DATE(created_at),SUM(prompt_tokens),SUM(completion_tokens),SUM(total_tokens),COUNT(*),MIN(created_at),MAX(created_at) FROM observation_invocation_summaries WHERE instance_id>? AND instance_id<=? GROUP BY instance_id,`+dimension+`,DATE(created_at)`, scope, prior, active)
		}
		exec(`INSERT INTO cost_records(trace_id,session_id,instance_id,invocation_id,provider_type,model_name,estimated_cost,recorded_at) SELECT trace_id,session_id,instance_id,id,'openai','m',0.01,created_at FROM observation_invocation_summaries WHERE instance_id>? AND instance_id<=?`, prior, active)
		exec(`INSERT INTO observation_cost_facts(id,invocation_id,instance_id,session_id,trace_id,currency,estimated_cost,eligible_at,resolved) SELECT cr.id,cr.invocation_id,cr.instance_id,cr.session_id,cr.trace_id,cr.currency,cr.estimated_cost,mi.created_at,TRUE FROM cost_records cr JOIN observation_invocation_summaries mi ON mi.id=cr.invocation_id WHERE cr.instance_id>? AND cr.instance_id<=?`, prior, active)
		exec(`INSERT INTO observation_session_cost_daily SELECT instance_id,session_id,DATE(eligible_at),currency,SUM(estimated_cost) FROM observation_cost_facts WHERE instance_id>? AND instance_id<=? GROUP BY instance_id,session_id,DATE(eligible_at),currency`, prior, active)
		if e = materializeCostOverview(context.Background(), s, "cr.instance_id>? AND cr.instance_id<=?", prior, active); e != nil {
			t.Fatal(e)
		}
		// Synthetic rows above already materialized both fact and day tables.
		// Acknowledge only source IDs with matching durable facts, in bounded
		// batches, as the ordinary projection transaction would do.
		for {
			res, err := s.SQL().Exec(`DELETE FROM observation_cost_pending WHERE id IN (SELECT id FROM (SELECT p.id FROM observation_cost_pending p JOIN cost_records c ON c.id=p.id JOIN observation_cost_facts f ON f.id=c.id AND f.trace_id=c.trace_id AND f.invocation_id=c.invocation_id AND f.estimated_cost=c.estimated_cost AND f.resolved=TRUE ORDER BY p.id LIMIT 500) verified)`)
			if err != nil {
				t.Fatal(err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
		prior = active
		exec(`ANALYZE TABLE observation_daily_summaries,observation_session_cost_daily,observation_invocation_summaries,observation_cost_overview_daily,cost_records`)
		if os.Getenv("CM_QUERY_CAPACITY_PLAN_ONLY") == "1" {
			if active == 10000 {
				captureQueryCapacityPlans(t, s, since, until)
			}
			continue
		}
		reporter := NewModelInvocationRepository(s).(*modelInvocationRepository)
		latencies := make([]time.Duration, 0, 20)
		for run := 0; run < 20; run++ {
			begin := time.Now()
			report, err := reporter.ListAdminSessionUsageReport(1, 20, "", SessionUsageFilter{Since: &since, Until: &until})
			latencies = append(latencies, time.Since(begin))
			if err != nil {
				t.Fatalf("active=%d run=%d: %v", active, run, err)
			}
			if report.Total != 10000 || report.Summary.TotalTokens != active*30*15 || math.Abs(report.Summary.EstimatedCost-float64(active)*30*0.01) > 0.000001 {
				t.Fatalf("capacity total mismatch: %+v", report.Summary)
			}
		}
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		t.Logf("MEASURED instances=10000 active=%d%% days=90 facts=%d rate=1/active-instance/day samples=20 P50=%s P95=%s max=%s", active/100, active*90, latencies[9], latencies[18], latencies[19])
		cte, args := sessionUsageReportSQL(0, "", SessionUsageFilter{Since: &since, Until: &until})
		pageSQL, args := adminSessionPageSQL(cte, args, "", 21, 0, SessionUsageFilter{Since: &since, Until: &until})
		row, err := s.SQL().QueryRowContext(context.Background(), "EXPLAIN FORMAT=JSON "+pageSQL, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		if err = row.Scan(&plan); err != nil {
			t.Fatal(err)
		}
		t.Logf("EXPLAIN active=%d: %s", active, plan)
		if active == 10000 {
			costs := NewCostRecordRepository(s).(*costRecordRepository)
			costLatency := make([]time.Duration, 0, 20)
			for run := 0; run < 20; run++ {
				begin := time.Now()
				report, err := costs.CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &since, Until: &until})
				costLatency = append(costLatency, time.Since(begin))
				if err != nil {
					t.Fatal(err)
				}
				if report.Total != active*30 || math.Abs(report.Totals.EstimatedCost-float64(active)*30*.01) > .000001 {
					t.Fatalf("cost capacity mismatch: %+v", report.Totals)
				}
			}
			sort.Slice(costLatency, func(i, j int) bool { return costLatency[i] < costLatency[j] })
			t.Logf("MEASURED cost overview ledger=900000 selected=300000 samples=20 P50=%s P95=%s max=%s", costLatency[9], costLatency[18], costLatency[19])
			row, err = s.SQL().QueryRow(`EXPLAIN FORMAT=JSON SELECT COUNT(*),SUM(cr.estimated_cost) FROM cost_records cr LEFT JOIN users u ON u.id=cr.user_id LEFT JOIN instances i ON i.id=cr.instance_id WHERE cr.recorded_at>=? AND cr.recorded_at<? AND cr.id<=900000`, since, until)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("EXPLAIN cost overview: %s", plan)
			for _, scenario := range []string{"instance-session", "trace-page", "admin-fractional-window", "cost-fractional-window"} {
				latencies := []time.Duration{}
				partialUntil := time.Now().UTC()
				partialSince := partialUntil.Add(-30 * 24 * time.Hour)
				for run := 0; run < 20; run++ {
					begin := time.Now()
					if scenario == "instance-session" {
						_, e = reporter.ListSessionUsageReport(10000, 1, 20, "", SessionUsageFilter{Since: &since, Until: &until})
					} else if scenario == "trace-page" {
						_, e = reporter.TracePage(context.Background(), "trace-10000", "", 200)
					} else if scenario == "admin-fractional-window" {
						var report *AdminSessionUsageReport
						report, e = reporter.ListAdminSessionUsageReport(1, 20, "", SessionUsageFilter{Since: &partialSince, Until: &partialUntil})
						if e == nil && report.Summary.TotalTokens != 10000*29*15 {
							t.Fatalf("partial token totals %+v", report.Summary)
						}
					} else {
						var report *CostOverviewReport
						report, e = costs.CostOverviewReport(1, 20, "", SessionUsageFilter{Since: &partialSince, Until: &partialUntil})
						if e == nil && report.Total != 10000*29 {
							t.Fatalf("partial cost count %d", report.Total)
						}
					}
					if e != nil {
						t.Fatal(e)
					}
					latencies = append(latencies, time.Since(begin))
				}
				sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
				t.Logf("MEASURED %s samples=20 P50=%s P95=%s max=%s", scenario, latencies[9], latencies[18], latencies[19])
			}
			captureQueryCapacityPlans(t, s, since, until)
		}
	}
	exec(`INSERT INTO model_invocations(trace_id,session_id,request_id,user_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,status,prompt_tokens,completion_tokens,total_tokens,created_at,request_payload,response_payload) SELECT trace_id,session_id,request_id,user_id,instance_id,provider_type,requested_model,actual_provider_model,'llm',status,prompt_tokens,completion_tokens,total_tokens,created_at,REPEAT('a',1024),REPEAT('b',2048) FROM observation_invocation_summaries ORDER BY id LIMIT 100`)
	row, e = s.SQL().QueryRow(`SELECT COUNT(*),AVG(OCTET_LENGTH(request_payload)+OCTET_LENGTH(response_payload)) FROM model_invocations`)
	if e != nil {
		t.Fatal(e)
	}
	var samples int
	var avg float64
	if e = row.Scan(&samples, &avg); e != nil {
		t.Fatal(e)
	}
	t.Log(fmt.Sprintf("MEASURED synthetic body samples=%d average combined bytes=%.0f (not production traffic)", samples, avg))
}

func captureQueryCapacityPlans(t *testing.T, s db.Session, since, until time.Time) {
	t.Helper()
	cte, args := sessionUsageReportSQL(0, "", SessionUsageFilter{Since: &since, Until: &until})
	pageSQL, pageArgs := adminSessionPageSQL(cte, append([]interface{}{}, args...), "", 21, 0, SessionUsageFilter{Since: &since, Until: &until})
	costFrom, costArgs := costOverviewRollupFrom("instance", since, until, 0, true, true)
	for _, query := range []struct {
		name, sql string
		args      []interface{}
	}{
		{"admin page", pageSQL, pageArgs},
		{"standalone global materialization", cte + ` SELECT COUNT(*),SUM(total_tokens),SUM(estimated_cost) FROM summaries`, args},
		{"cost instance aggregation", "SELECT " + costInstanceLabel + "," + costSums + costFrom + " GROUP BY 1 ORDER BY SUM(cr.estimated_cost) DESC LIMIT 6", costArgs},
	} {
		row, e := s.SQL().QueryRowContext(context.Background(), "EXPLAIN ANALYZE "+query.sql, query.args...)
		if e != nil {
			t.Fatal(e)
		}
		var plan string
		if e = row.Scan(&plan); e != nil {
			t.Fatal(e)
		}
		t.Logf("ANALYZE %s: %s", query.name, plan)
	}
	rows, e := s.SQL().Query(`SELECT table_name,table_rows,data_length,index_length FROM information_schema.tables WHERE table_schema=DATABASE() AND (table_name LIKE 'observation_%' OR table_name='cost_records') ORDER BY table_name`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var estimated, data, index int64
		if e = rows.Scan(&name, &estimated, &data, &index); e != nil {
			t.Fatal(e)
		}
		t.Logf("MEASURED table=%s estimated_rows=%d data_bytes=%d index_bytes=%d", name, estimated, data, index)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
}
