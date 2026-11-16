package admindashboard

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/upper/db/v4/adapter/mysql"
	"io"
	"strings"
	"testing"
	"time"
)

type connector struct {
	queries             *[]string
	args                *[][]driver.NamedValue
	fail                string
	zero                bool
	epoch               *time.Time
	gatewayTurns        bool
	instanceScanFailure bool
}

func (c connector) Connect(context.Context) (driver.Conn, error) { return conn{c}, nil }
func (c connector) Driver() driver.Driver                        { return drv{} }

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return nil, errors.New("connector required") }

type conn struct{ connector }

func (conn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare") }
func (conn) Close() error                        { return nil }
func (conn) Begin() (driver.Tx, error)           { return nil, errors.New("transaction") }
func (c conn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	*c.queries = append(*c.queries, q)
	*c.args = append(*c.args, a)
	if c.fail != "" && strings.Contains(q, c.fail) {
		return nil, errors.New("forced read failure")
	}
	if c.instanceScanFailure && strings.Contains(q, "SELECT type,COUNT(*)") {
		return &rows{v: [][]driver.Value{{"partial-type", int64(7)}, {"bad", "invalid-count"}}}, nil
	}
	if c.zero {
		switch {
		case strings.Contains(q, "COUNT(DISTINCT user_id)"):
			return &rows{v: [][]driver.Value{{int64(0), int64(0), int64(0), nil, nil}}}, nil
		case strings.Contains(q, "FROM users") || strings.Contains(q, "AS missing"):
			return &rows{v: [][]driver.Value{{int64(0)}}}, nil
		case strings.Contains(q, "LEFT JOIN instance_model_activity"):
			return &rows{v: [][]driver.Value{{int64(0), int64(0)}}}, nil
		case strings.Contains(q, "GROUP BY"):
			return &rows{}, nil
		}
	}
	switch {
	case strings.Contains(q, "DATABASE()"):
		return &rows{v: [][]driver.Value{{"test"}}}, nil
	case strings.Contains(q, "COUNT(DISTINCT user_id)"):
		return &rows{v: [][]driver.Value{{int64(1800), int64(2), int64(3), float64(75), float64(12)}}}, nil
	case strings.Contains(q, "FROM users"):
		return &rows{v: [][]driver.Value{{int64(10)}}}, nil
	case strings.Contains(q, "LEFT JOIN instance_model_activity"):
		return &rows{v: [][]driver.Value{{int64(8), int64(1)}}}, nil
	case strings.Contains(q, "GROUP BY currency"):
		return &rows{v: [][]driver.Value{{"USD", float64(2), int64(0)}, {"CNY", float64(9), int64(1)}}}, nil
	case strings.Contains(q, "AS missing"):
		return &rows{v: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "AS model_name"):
		return &rows{v: [][]driver.Value{{"model-a", int64(1800)}}}, nil
	case strings.Contains(q, "TIMESTAMPDIFF") && strings.Contains(q, "FROM model_invocations WHERE"):
		return &rows{v: [][]driver.Value{{int64(0), int64(1800)}}}, nil
	case strings.Contains(q, "FROM chat_messages u"):
		if c.gatewayTurns {
			return &rows{v: [][]driver.Value{{int64(0), int64(8)}}}, nil
		}
		return &rows{v: [][]driver.Value{{int64(0), int64(2)}}}, nil
	case strings.Contains(q, "analytics_epoch"):
		if c.epoch != nil {
			return &rows{v: [][]driver.Value{{*c.epoch}}}, nil
		}
		return &rows{v: [][]driver.Value{{time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)}}}, nil
	case strings.Contains(q, "question_v4"):
		if c.epoch != nil {
			return &rows{v: [][]driver.Value{{int64(3), int64(2)}}}, nil
		}
		return &rows{v: [][]driver.Value{{int64(0), int64(2)}}}, nil
	case strings.Contains(q, "FROM instances"):
		return &rows{v: [][]driver.Value{{"openclaw", int64(8)}}}, nil
	case strings.Contains(q, "FROM runtime_pods"):
		return &rows{v: [][]driver.Value{{"openclaw", int64(3), int64(2)}}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}

type rows struct {
	v     [][]driver.Value
	width int
}

func (r *rows) Columns() []string {
	if r.width == 0 && len(r.v) > 0 {
		r.width = len(r.v[0])
	}
	return make([]string, r.width)
}

func (r *rows) Close() error { return nil }
func (r *rows) Next(d []driver.Value) error {
	if len(r.v) == 0 {
		return io.EOF
	}
	copy(d, r.v[0])
	r.v = r.v[1:]
	return nil
}
func TestFullPeriodAggregationAndShanghaiWindow(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	queries = nil
	args = nil
	now := time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)
	d := NewStore(sess).Overview(context.Background(), now)
	if d.Window.Start.Format(time.RFC3339) != "2026-11-15T16:00:00Z" || !d.Window.End.Equal(now) {
		t.Fatalf("window %+v", d.Window)
	}
	if d.Usage.TotalTokens == nil || *d.Usage.TotalTokens != 1800 || len(d.Models) != 1 || d.Models[0].Tokens != 1800 {
		t.Fatalf("totals %+v", d)
	}
	if len(d.Usage.Currencies) != 2 || d.Usage.Currencies[0].Complete || d.Usage.Currencies[1].Complete {
		t.Fatalf("missing association must mark every subtotal incomplete: %+v", d.Usage)
	}
	if d.Usage.Conversations == nil || *d.Usage.Conversations != 2 || d.Coverage["conversations"].Status != "partial" {
		t.Fatalf("conversations %+v", d)
	}
	if d.IdleInstances == nil || *d.IdleInstances != 1 {
		t.Fatalf("idle %+v", d)
	}
	for _, q := range queries {
		if strings.Contains(strings.ToUpper(q), "LIMIT ") {
			t.Fatalf("sampled query: %s", q)
		}
	}
	for i, q := range queries {
		if strings.Contains(q, "FROM model_invocations WHERE status") {
			foundStart, foundEnd := false, false
			for _, a := range args[i] {
				if at, ok := a.Value.(time.Time); ok {
					foundStart = foundStart || at.Equal(d.Window.Start)
					foundEnd = foundEnd || at.Equal(now)
				}
			}
			if !foundStart || !foundEnd {
				t.Fatalf("query missing exclusive window args: %s", q)
			}
		}
	}
	joined := strings.Join(queries, "\n")
	for _, required := range []string{"status IN ('completed','failed')", "traffic_class='llm'", "a.generation=i.runtime_generation", "a.stop_phase='completed'", "recorded_at>=?", "GROUP BY currency"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing predicate %s", required)
		}
	}
}

func TestUnavailableSourcesRemainNull(t *testing.T) {
	now := time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)
	d := NewStore(nil).Overview(context.Background(), now)
	if d.TotalUsers != nil || d.TotalInstances != nil || d.ActiveUsers != nil || d.IdleInstances != nil || d.Usage.TotalTokens != nil || d.Usage.Conversations != nil {
		t.Fatalf("invented zero %+v", d)
	}
	for _, p := range d.History {
		if p.Tokens != nil || p.Conversations != nil {
			t.Fatalf("invented history %+v", p)
		}
	}
	for _, c := range d.Coverage {
		if c.Status != "unavailable" {
			t.Fatalf("coverage %+v", c)
		}
	}
	if d.Models == nil || d.Types == nil || d.Usage.Currencies == nil {
		t.Fatal("arrays must serialize empty, not null")
	}
}

func TestRuntimeInventoryCountsActualPodsAndUnknownTypes(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	now := time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	store := NewStore(sess)
	store.SetRuntimeReader(func(context.Context) ([]models.RuntimePod, error) {
		return []models.RuntimePod{
			{RuntimeType: "openclaw", State: "ready", LastSeenAt: &now},
			{RuntimeType: "openclaw", State: "ready", LastSeenAt: &old},
			{RuntimeType: "new-runtime", State: "unknown"},
		}, nil
	})
	d := store.Overview(context.Background(), now)
	counts := map[string]Type{}
	for _, v := range d.Types {
		counts[v.Type] = v
	}
	if counts["openclaw"].RuntimePods != 2 || counts["openclaw"].ReadyPods != 1 || counts["new-runtime"].RuntimePods != 1 {
		t.Fatalf("inventory %+v", counts)
	}
	if d.Coverage["runtimes"].Status != "ready" {
		t.Fatalf("coverage %+v", d.Coverage)
	}
	for _, q := range queries {
		if strings.Contains(q, "FROM runtime_pods") {
			t.Fatal("reader replaced with DB approximation")
		}
	}
}
func TestFailedUsageReadDoesNotExposePartialTotals(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args, fail: "AS model_name"}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	d := NewStore(sess).Overview(context.Background(), time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC))
	if d.Usage.TotalTokens != nil || d.ActiveUsers != nil || len(d.Models) != 0 || d.Coverage["usage"].Status != "unavailable" {
		t.Fatalf("partial source leaked %+v", d)
	}
	for _, p := range d.History {
		if p.Tokens != nil {
			t.Fatal("partial token history")
		}
	}
	if d.TotalUsers == nil || d.Usage.Conversations == nil {
		t.Fatal("independent sources should remain available")
	}
}

func TestObservedEmptyPeriodIsZeroAndUndefinedRatesStayNull(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args, zero: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	d := NewStore(sess).Overview(context.Background(), time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC))
	if d.Usage.TotalTokens == nil || *d.Usage.TotalTokens != 0 || d.Usage.Conversations == nil || *d.Usage.Conversations != 0 || d.ActiveUsers == nil || *d.ActiveUsers != 0 {
		t.Fatalf("known empty %+v", d)
	}
	if d.Usage.SuccessRate != nil || d.Usage.AverageResponseMS != nil {
		t.Fatal("undefined denominator must remain null")
	}
	for _, p := range d.History {
		if p.Tokens == nil || *p.Tokens != 0 || p.Conversations == nil || *p.Conversations != 0 {
			t.Fatalf("empty point %+v", p)
		}
	}
}

func TestRuntimeFailurePreservesInstanceDistribution(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	store := NewStore(sess)
	store.SetRuntimeReader(func(context.Context) ([]models.RuntimePod, error) { return nil, errors.New("inventory unavailable") })
	d := store.Overview(context.Background(), time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC))
	var instances int64
	for _, v := range d.Types {
		instances += v.Instances
	}
	if instances != 8 || len(d.Types) < 5 {
		t.Fatalf("healthy instance distribution discarded: %+v", d.Types)
	}
	if d.Coverage["instances"].Status != "ready" || d.Coverage["runtimes"].Status != "unavailable" {
		t.Fatalf("source coverage %+v", d.Coverage)
	}
}

func TestInstanceGroupingFailurePreservesRuntimeDistribution(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args, fail: "SELECT type,COUNT(*)"}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	now := time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)
	called := false
	store := NewStore(sess)
	store.SetRuntimeReader(func(context.Context) ([]models.RuntimePod, error) {
		called = true
		return []models.RuntimePod{{RuntimeType: "openclaw", State: "ready", LastSeenAt: &now}, {RuntimeType: "new-runtime", State: "unknown"}}, nil
	})
	d := store.Overview(context.Background(), now)
	if !called {
		t.Fatal("instance grouping failure prevented independent Runtime read")
	}
	counts := map[string]Type{}
	for _, v := range d.Types {
		counts[v.Type] = v
	}
	if counts["openclaw"].RuntimePods != 1 || counts["openclaw"].ReadyPods != 1 || counts["new-runtime"].RuntimePods != 1 {
		t.Fatalf("healthy Runtime distribution discarded: %+v", d.Types)
	}
	if d.Coverage["instances"].Status != "unavailable" || d.Coverage["runtimes"].Status != "ready" {
		t.Fatalf("source coverage %+v", d.Coverage)
	}
	for _, v := range d.Types {
		if v.Instances != 0 {
			t.Fatal("failed instance query leaked partial counts")
		}
	}
}

func TestInstanceGroupingScanFailureDiscardsPrefixAndPreservesRuntime(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args, instanceScanFailure: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	now := time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)
	called := false
	store := NewStore(sess)
	store.SetRuntimeReader(func(context.Context) ([]models.RuntimePod, error) {
		called = true
		return []models.RuntimePod{{RuntimeType: "openclaw", State: "ready", LastSeenAt: &now}}, nil
	})
	d := store.Overview(context.Background(), now)
	if !called || d.Coverage["instances"].Status != "unavailable" || d.Coverage["runtimes"].Status != "ready" {
		t.Fatalf("independent source lost: called=%v coverage=%+v", called, d.Coverage)
	}
	var pods int64
	for _, v := range d.Types {
		pods += v.RuntimePods
		if v.Instances != 0 || v.Type == "partial-type" {
			t.Fatalf("successful prefix leaked: %+v", d.Types)
		}
	}
	if pods != 1 {
		t.Fatalf("Runtime total %d", pods)
	}
}

func TestAdminConversationIncludesOwnerlessGatewayTurns(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args, fail: "northbound_owner_records", gatewayTurns: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	d := NewStore(sess).Overview(context.Background(), time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC))
	if d.Usage.Conversations == nil || *d.Usage.Conversations != 8 {
		t.Fatalf("ownerless gateway conversations discarded: %+v", d.Usage.Conversations)
	}
	var sum int64
	for _, point := range d.History {
		if point.Conversations == nil {
			t.Fatal("durable platform history must not depend on owner growth epoch")
		}
		sum += *point.Conversations
	}
	if sum != 8 {
		t.Fatalf("hourly total=%d", sum)
	}
}
func TestAdminConversationReadFailureRemainsUnknown(t *testing.T) {
	var queries []string
	var args [][]driver.NamedValue
	sess, err := mysql.New(sql.OpenDB(connector{queries: &queries, args: &args, fail: "FROM chat_messages u"}))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	d := NewStore(sess).Overview(context.Background(), time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC))
	if d.Usage.Conversations != nil || d.Coverage["conversations"].Status != "unavailable" {
		t.Fatalf("missing chat source fabricated a count: %+v", d.Usage)
	}
	for _, point := range d.History {
		if point.Conversations != nil {
			t.Fatal("missing chat history fabricated a zero")
		}
	}
}
