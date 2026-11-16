// Package admindashboard provides bounded platform aggregates, never sampled lists.
package admindashboard

import (
	"context"
	"database/sql"
	"sort"
	"time"
	_ "time/tzdata"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type Coverage struct {
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type Currency struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
	Complete bool    `json:"complete"`
}
type Usage struct {
	TotalTokens       *int64     `json:"total_tokens"`
	Conversations     *int64     `json:"conversations"`
	SuccessRate       *float64   `json:"success_rate"`
	AverageResponseMS *float64   `json:"average_response_ms"`
	Currencies        []Currency `json:"currencies"`
}
type Point struct {
	At            time.Time `json:"at"`
	Tokens        *int64    `json:"tokens"`
	Conversations *int64    `json:"conversations"`
}
type Model struct {
	Name   string `json:"name"`
	Tokens int64  `json:"tokens"`
}
type Type struct {
	Type        string `json:"type"`
	Instances   int64  `json:"instances"`
	RuntimePods int64  `json:"runtime_pods"`
	ReadyPods   int64  `json:"ready_pods"`
}
type Overview struct {
	Timezone        string              `json:"timezone"`
	Window          Window              `json:"window"`
	TotalUsers      *int64              `json:"total_users"`
	TotalInstances  *int64              `json:"total_instances"`
	ActiveUsers     *int64              `json:"active_users"`
	ActiveInstances *int64              `json:"active_instances"`
	IdleInstances   *int64              `json:"idle_instances"`
	IdleTotal       *int64              `json:"idle_total"`
	Usage           Usage               `json:"usage"`
	History         []Point             `json:"history"`
	Models          []Model             `json:"models"`
	Types           []Type              `json:"types"`
	Coverage        map[string]Coverage `json:"coverage"`
}
type Store struct {
	sess          db.Session
	runtimeReader func(context.Context) ([]models.RuntimePod, error)
}

func NewStore(sess db.Session) *Store { return &Store{sess: sess} }
func (s *Store) SetRuntimeReader(reader func(context.Context) ([]models.RuntimePod, error)) {
	s.runtimeReader = reader
}
func ptr[T any](v T) *T { return &v }

const invScope = ` FROM model_invocations WHERE status IN ('completed','failed') AND traffic_class='llm' AND created_at>=? AND created_at<? `

func (s *Store) Overview(ctx context.Context, now time.Time) *Overview {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).UTC()
	d := &Overview{Timezone: "Asia/Shanghai", Window: Window{start, now.UTC()}, Usage: Usage{Currencies: []Currency{}}, History: []Point{}, Models: []Model{}, Types: []Type{}, Coverage: map[string]Coverage{}}
	for _, key := range []string{"instances", "usage", "costs", "conversations", "runtimes"} {
		d.Coverage[key] = Coverage{Status: "unavailable", Note: "统计暂不可用"}
	}
	for at := start; at.Before(now); at = at.Add(time.Hour) {
		d.History = append(d.History, Point{At: at})
	}
	if s.sess == nil {
		return d
	}
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	var count int64
	if err == nil && row.Scan(&count) == nil {
		d.TotalUsers = ptr(count)
	}
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(i.status='stopped' AND a.stopped_reason='idle_timeout' AND a.stop_phase='completed'),0) FROM instances i LEFT JOIN instance_model_activity a ON a.instance_id=i.id AND a.generation=i.runtime_generation WHERE i.status<>'deleted'`)
	var idle int64
	if err == nil && row.Scan(&count, &idle) == nil {
		d.TotalInstances = ptr(count)
		d.IdleTotal = ptr(count)
		d.IdleInstances = ptr(idle)
		d.Coverage["instances"] = Coverage{Status: "ready"}
	}
	s.usage(ctx, d)
	s.costs(ctx, d)
	s.conversations(ctx, d)
	s.types(ctx, d)
	return d
}
func (s *Store) usage(ctx context.Context, d *Overview) {
	w := d.Window
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(total_tokens),0),COUNT(DISTINCT user_id),COUNT(DISTINCT instance_id),100.0*SUM(status='completed')/NULLIF(COUNT(*),0),AVG(CASE WHEN status='completed' AND latency_ms>=0 THEN latency_ms END)`+invScope, w.Start, w.End)
	var tokens, users, instances int64
	var success, latency sql.NullFloat64
	if err != nil || row.Scan(&tokens, &users, &instances, &success, &latency) != nil {
		return
	}
	models := []Model{}
	err = s.read(ctx, `SELECT COALESCE(NULLIF(actual_provider_model,''),NULLIF(requested_model,''),'unknown') AS model_name,COALESCE(SUM(total_tokens),0)`+invScope+` GROUP BY model_name ORDER BY 2 DESC,1`, []any{w.Start, w.End}, func(rows *sql.Rows) error {
		var m Model
		if err := rows.Scan(&m.Name, &m.Tokens); err != nil {
			return err
		}
		models = append(models, m)
		return nil
	})
	if err != nil {
		return
	}
	history := make([]int64, len(d.History))
	err = s.read(ctx, `SELECT TIMESTAMPDIFF(HOUR,?,created_at),COALESCE(SUM(total_tokens),0)`+invScope+` GROUP BY 1`, []any{w.Start, w.Start, w.End}, func(rows *sql.Rows) error {
		var i int
		var n int64
		if err := rows.Scan(&i, &n); err != nil {
			return err
		}
		if i >= 0 && i < len(history) {
			history[i] = n
		}
		return nil
	})
	if err != nil {
		return
	}
	d.Usage.TotalTokens = ptr(tokens)
	d.ActiveUsers = ptr(users)
	d.ActiveInstances = ptr(instances)
	d.Models = models
	if success.Valid {
		d.Usage.SuccessRate = ptr(success.Float64)
	}
	if latency.Valid {
		d.Usage.AverageResponseMS = ptr(latency.Float64)
	}
	for i := range history {
		d.History[i].Tokens = ptr(history[i])
	}
	d.Coverage["usage"] = Coverage{Status: "ready", Note: "LLM 已结束调用尝试，按创建时间；不含历史空流量类型、pending、blocked；成功率 completed/(completed+failed)，响应为成功调用平均耗时"}
}
func (s *Store) read(ctx context.Context, q string, args []any, scan func(*sql.Rows) error) error {
	rows, err := s.sess.SQL().QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (s *Store) costs(ctx context.Context, d *Overview) {
	w := d.Window
	var missing int64
	// Unassociated or unpriced calls make every currency subtotal incomplete.
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) AS missing FROM model_invocations v WHERE v.status IN ('completed','failed') AND v.traffic_class='llm' AND v.created_at>=? AND v.created_at<? AND NOT EXISTS(SELECT 1 FROM cost_records c WHERE c.invocation_id=v.id AND c.recorded_at>=? AND c.recorded_at<? AND c.currency<>'' AND (c.actual_cost IS NOT NULL OR c.input_unit_price>0 OR c.output_unit_price>0))`, w.Start, w.End, w.Start, w.End)
	if err != nil || row.Scan(&missing) != nil {
		return
	}
	currencies := []Currency{}
	partial := missing > 0
	err = s.read(ctx, `SELECT currency,COALESCE(SUM(CASE WHEN actual_cost IS NOT NULL THEN actual_cost WHEN input_unit_price>0 OR output_unit_price>0 THEN estimated_cost ELSE 0 END),0),SUM(currency='' OR invocation_id IS NULL OR (actual_cost IS NULL AND input_unit_price<=0 AND output_unit_price<=0)) FROM cost_records WHERE recorded_at>=? AND recorded_at<? GROUP BY currency ORDER BY currency`, []any{w.Start, w.End}, func(rows *sql.Rows) error {
		var c Currency
		var unknown int64
		if err := rows.Scan(&c.Currency, &c.Amount, &unknown); err != nil {
			return err
		}
		c.Complete = unknown == 0 && missing == 0
		if !c.Complete {
			partial = true
		}
		currencies = append(currencies, c)
		return nil
	})
	if err != nil {
		return
	}
	d.Usage.Currencies = currencies
	status := "ready"
	if partial {
		status = "partial"
	}
	d.Coverage["costs"] = Coverage{Status: status, Note: "按费用记录时间及币种汇总；缺少价格或调用关联时仅为已记录小计"}
}
func (s *Store) types(ctx context.Context, d *Overview) {
	values := map[string]*Type{}
	for _, name := range []string{"openclaw", "hermes", "opencode", "workbuddy", "deepseek-harness"} {
		values[name] = &Type{Type: name}
	}
	get := func(name string) *Type {
		if name == "" {
			name = "unknown"
		}
		if values[name] == nil {
			values[name] = &Type{Type: name}
		}
		return values[name]
	}
	instanceCounts := map[string]int64{}
	err := s.read(ctx, `SELECT type,COUNT(*) FROM instances WHERE status<>'deleted' GROUP BY type`, nil, func(rows *sql.Rows) error {
		var name string
		var count int64
		if err := rows.Scan(&name, &count); err != nil {
			return err
		}
		instanceCounts[name] = count
		return nil
	})
	if err != nil {
		d.Coverage["instances"] = Coverage{Status: "unavailable", Note: "实例分类统计暂不可用"}
	} else {
		for name, count := range instanceCounts {
			get(name).Instances = count
		}
	}
	// Runtime inventory is independent of instance SQL, including scan failures.
	err = nil
	publish := func() {
		d.Types = d.Types[:0]
		for _, v := range values {
			d.Types = append(d.Types, *v)
		}
		sort.Slice(d.Types, func(i, j int) bool { return d.Types[i].Type < d.Types[j].Type })
	}
	// Instance grouping is an independent source; Runtime failure must not erase it.
	publish()
	runtimeCoverage := Coverage{Status: "partial", Note: "非删除 Runtime 注册记录；在线为心跳窗口内 ready/draining；不含未注册的集群 Pod"}
	if s.runtimeReader != nil {
		pods, readErr := s.runtimeReader(ctx)
		if readErr != nil {
			return
		}
		for _, pod := range pods {
			v := get(pod.RuntimeType)
			v.RuntimePods++
			if (pod.State == "ready" || pod.State == "draining") && pod.LastSeenAt != nil && !pod.LastSeenAt.Before(d.Window.End.Add(-heartbeat.Timeout)) {
				v.ReadyPods++
			}
		}
		runtimeCoverage = Coverage{Status: "ready", Note: "当前 Kubernetes Runtime Pod 清单；在线需当前 UID 的 ready/draining 注册及有效心跳"}
	} else {
		err = s.read(ctx, `SELECT runtime_type,COUNT(*),COALESCE(SUM(state IN ('ready','draining') AND last_seen_at>=?),0) FROM runtime_pods WHERE state<>'deleted' GROUP BY runtime_type`, []any{d.Window.End.Add(-heartbeat.Timeout)}, func(rows *sql.Rows) error {
			var name string
			var count, ready int64
			if err := rows.Scan(&name, &count, &ready); err != nil {
				return err
			}
			v := get(name)
			v.RuntimePods = count
			v.ReadyPods = ready
			return nil
		})
	}

	if err != nil {
		return
	}
	publish()
	d.Coverage["runtimes"] = runtimeCoverage
}
