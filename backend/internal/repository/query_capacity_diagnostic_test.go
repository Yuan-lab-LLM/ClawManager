package repository

import (
	"context"
	"fmt"
	"github.com/upper/db/v4/adapter/mysql"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// Read-only follow-up on a retained synthetic capacity fixture, never app data.
func TestQueryCapacityDiagnostic(t *testing.T) {
	name := os.Getenv("CM_QUERY_DIAGNOSTIC_DB")
	if name == "" {
		t.Skip("explicit synthetic fixture required")
	}
	if !strings.HasPrefix(name, "cm_database_test_") {
		t.Fatal("not isolated fixture")
	}
	s, e := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:" + os.Getenv("TEST_DATABASE_MYSQL_PORT"), Database: name, User: "root"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	row, e := s.SQL().QueryRow(`SELECT COUNT(*) FROM query_capacity_digits`)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	if e = row.Scan(&n); e != nil || n != 10 {
		t.Fatal("fixture marker", e)
	}
	until := time.Now().UTC().Truncate(24 * time.Hour)
	since := until.Add(-30 * 24 * time.Hour)
	filter := SessionUsageFilter{Since: &since, Until: &until}
	for _, scenario := range []string{"admin", "cost", "instance", "trace"} {
		durations := []time.Duration{}
		for run := 0; run < 20; run++ {
			start := time.Now()
			switch scenario {
			case "admin":
				_, e = NewModelInvocationRepository(s).(*modelInvocationRepository).ListAdminSessionUsageReport(1, 20, "", filter)
			case "cost":
				_, e = NewCostRecordRepository(s).(*costRecordRepository).CostOverviewReport(1, 20, "", filter)
			case "instance":
				_, e = NewModelInvocationRepository(s).(*modelInvocationRepository).ListSessionUsageReport(1000, 1, 20, "", filter)
			case "trace":
				_, e = NewModelInvocationRepository(s).(*modelInvocationRepository).TracePage(context.Background(), "trace-1000", "", 200)
			}
			if e != nil {
				t.Fatal(e)
			}
			durations = append(durations, time.Since(start))
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		t.Logf("DIAGNOSTIC %s P50=%s P95=%s max=%s", scenario, durations[9], durations[18], durations[19])
	}
	cte, args := sessionUsageReportSQL(0, "", filter)
	for _, kind := range []string{"total", "page"} {
		q := cte + ` SELECT COUNT(*),SUM(total_tokens),SUM(estimated_cost) FROM summaries`
		a := args
		if kind == "page" {
			q, a = adminSessionPageSQL(cte, args, "", 21, 0, filter)
		}
		row, e = s.SQL().QueryRowContext(context.Background(), "EXPLAIN ANALYZE "+q, a...)
		if e != nil {
			t.Fatal(e)
		}
		var plan string
		if e = row.Scan(&plan); e != nil {
			t.Fatal(e)
		}
		t.Log(fmt.Sprintf("ANALYZE %s: %s", kind, plan))
	}
}
