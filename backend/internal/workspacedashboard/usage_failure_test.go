package workspacedashboard

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/upper/db/v4/adapter/mysql"
)

// A database/sql stream drives Store.usage's real Scan/Err paths, including a
// valid prefix before the fault. No live database can reliably force both faults.
type usageFaultConnector struct {
	mode    string
	at      time.Time
	queries *[]string
}

func (c usageFaultConnector) Connect(context.Context) (driver.Conn, error) {
	return usageFaultConn{c}, nil
}
func (c usageFaultConnector) Driver() driver.Driver { return usageFaultDriver{} }

type usageFaultDriver struct{}

func (usageFaultDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type usageFaultConn struct{ usageFaultConnector }

func (usageFaultConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (usageFaultConn) Close() error              { return nil }
func (usageFaultConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c usageFaultConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	query = strings.Join(strings.Fields(query), " ")
	if c.queries != nil {
		*c.queries = append(*c.queries, query)
	}
	if query == "SELECT DATABASE() AS name" {
		return &usageFaultRows{columns: []string{"name"}, values: [][]driver.Value{{"clawmanager_test_faults"}}}, nil
	}
	if strings.HasPrefix(query, "SELECT created_at>=?") {
		return &usageFaultRows{columns: []string{"current", "tokens", "prompt", "completion", "calls"}, values: [][]driver.Value{{true, int64(33), int64(11), int64(22), int64(2)}}}, nil
	}
	if strings.HasPrefix(query, "SELECT CAST(") {
		if c.mode == "query" {
			return nil, errors.New("history query failed")
		}
		rows := &usageFaultRows{columns: []string{"at", "tokens", "calls"}, values: [][]driver.Value{{c.at, int64(12), int64(1)}}}
		switch c.mode {
		case "scan":
			rows.values = append(rows.values, []driver.Value{c.at.AddDate(0, 0, 1), "invalid-token-number", int64(1)})
		case "stream":
			rows.failure = errors.New("history stream interrupted")
		}
		return rows, nil
	}
	if strings.HasPrefix(query, "SELECT instance_id,") {
		return &usageFaultRows{columns: []string{"instance_id", "tokens"}, values: [][]driver.Value{{int64(1), int64(33)}}}, nil
	}
	if strings.HasPrefix(query, "WITH inv AS (") {
		return &usageFaultRows{columns: []string{"unused"}}, nil
	}
	if strings.HasPrefix(query, "SELECT MAX(v.created_at>=?") {
		return &usageFaultRows{columns: []string{"comparison", "current"}, values: [][]driver.Value{{false, false}}}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}

func TestUsageDashboardOnlyReadsUsageSources(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 15, 0, 0, time.UTC)
	for _, period := range []string{"today", "7d", "30d"} {
		t.Run(period, func(t *testing.T) {
			w, _ := makeWindow(period, now)
			queries := []string{}
			sess, err := mysql.New(sql.OpenDB(usageFaultConnector{mode: "complete", at: w.Start, queries: &queries}))
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			queries = nil // Adapter setup is outside the request.
			store := NewStore(sess)
			store.SetCapacityReader(func(context.Context) (map[string]int, error) {
				t.Error("usage request queried capacity")
				return nil, nil
			})
			d, err := store.Usage(context.Background(), 1, period, now)
			if err != nil {
				t.Fatal(err)
			}
			if d.Range != period || d.Timezone != "Asia/Shanghai" || !d.AsOf.Equal(now) || d.Window != w {
				t.Fatalf("incorrect window: %+v", d)
			}
			if d.Usage.TotalTokens == nil || *d.Usage.TotalTokens != 33 || len(d.AgentUsage) != 1 || d.AgentUsage[0].Tokens != 33 {
				t.Fatalf("aggregator not reused: %+v", d)
			}
			if d.Coverage.Usage.Status != "ready" || d.Coverage.Costs.Status != "partial" {
				t.Fatalf("incorrect coverage: %+v", d.Coverage)
			}
			if len(queries) != 7 {
				t.Fatalf("expected seven bounded usage reads, got %d: %v", len(queries), queries)
			}
			for _, query := range queries {
				for _, forbidden := range []string{"workspace_dashboard_observations", "gateway_runtime_tasks", "team_tasks", "chat_sessions", "user_quotas", "instance_model_activity", "INSERT", "UPDATE", "DELETE"} {
					if strings.Contains(query, forbidden) {
						t.Errorf("unrelated source %q: %s", forbidden, query)
					}
				}
			}
			if strings.Contains(queries[len(queries)-1], "DATE_FORMAT") {
				t.Fatal("usage attribution collected annual activity")
			}
		})
	}
	store := NewStore(nil)
	for _, tc := range []struct {
		user   int
		period string
	}{{0, "7d"}, {-1, "7d"}, {1, "all"}, {1, ""}} {
		if _, err := store.Usage(context.Background(), tc.user, tc.period, now); err == nil {
			t.Fatalf("accepted invalid request: %+v", tc)
		}
	}
}

type usageFaultRows struct {
	columns []string
	values  [][]driver.Value
	index   int
	failure error
}

func (r *usageFaultRows) Columns() []string { return r.columns }
func (r *usageFaultRows) Close() error      { return nil }
func (r *usageFaultRows) Next(values []driver.Value) error {
	if r.index == len(r.values) {
		if r.failure != nil {
			return r.failure
		}
		return io.EOF
	}
	copy(values, r.values[r.index])
	r.index++
	return nil
}

func TestUsageHistoryReadFailuresKeepUnknownBinsAndPeriodTotals(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 15, 0, 0, time.UTC)
	w, _ := makeWindow("7d", now)
	for _, mode := range []string{"query", "scan", "stream", "complete"} {
		t.Run(mode, func(t *testing.T) {
			sqldb := sql.OpenDB(usageFaultConnector{mode: mode, at: w.Start})
			sess, err := mysql.New(sqldb)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			d := emptyDashboard("7d", now, w)
			NewStore(sess).usage(context.Background(), 1, d)
			if d.Usage.TotalTokens == nil || *d.Usage.TotalTokens != 33 || d.Usage.InvocationCount == nil || *d.Usage.InvocationCount != 2 || d.Usage.PreviousTokens == nil || *d.Usage.PreviousTokens != 0 {
				t.Fatalf("independently completed totals lost: %+v", d.Usage)
			}
			if mode == "complete" {
				if d.Coverage.Usage.Status != "ready" || d.UsageHistory[0].Tokens == nil || *d.UsageHistory[0].Tokens != 12 || d.UsageHistory[0].Calls == nil || *d.UsageHistory[0].Calls != 1 {
					t.Fatalf("valid history lost: %+v", d)
				}
				for _, p := range d.UsageHistory[1:] {
					if p.Tokens == nil || *p.Tokens != 0 || p.Calls == nil || *p.Calls != 0 {
						t.Errorf("successful empty bucket not zero: %+v", p)
					}
				}
			} else {
				if d.Coverage.Usage.Status != "partial" {
					t.Errorf("history failure coverage: %+v", d.Coverage.Usage)
				}
				for _, p := range d.UsageHistory {
					if p.Tokens != nil || p.Calls != nil {
						t.Errorf("interrupted history presented known bin: %+v", p)
					}
				}
			}
		})
	}
}
