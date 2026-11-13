package workspacedashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
)

var shanghai, _ = time.LoadLocation("Asia/Shanghai")
var states = []string{"busy", "idle", "running", "sleeping", "starting", "failed", "offline", "unknown"}

type CapacityReader func(context.Context) (map[string]int, error)
type Store struct {
	sess              db.Session
	capacity          CapacityReader
	observationCursor int // accessed only by the lifecycle observation worker
}

func NewStore(sess db.Session) *Store                  { return &Store{sess: sess} }
func (s *Store) SetCapacityReader(read CapacityReader) { s.capacity = read }
func makeWindow(period string, now time.Time) (Window, error) {
	days := 7
	switch period {
	case "today":
		days = 1
	case "7d":
	case "30d":
		days = 30
	default:
		return Window{}, fmt.Errorf("invalid range")
	}
	local := now.In(shanghai)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghai).AddDate(0, 0, 1-days)
	return Window{Start: start.UTC(), End: now.UTC(), PreviousStart: start.AddDate(0, 0, -days).UTC(), PreviousEnd: local.AddDate(0, 0, -days).UTC()}, nil
}
func emptyDashboard(period string, now time.Time, w Window) *Dashboard {
	d := &Dashboard{Range: period, Timezone: "Asia/Shanghai", AsOf: now.UTC(), Window: w, Instances: []Agent{}, Tasks: []Task{}, StateCounts: map[string]int{}, Usage: Usage{Currencies: []CurrencyUsage{}}, UsageHistory: []UsagePoint{}, AgentUsage: []AgentUsage{}, Capacity: Capacity{Runtimes: []RuntimeCapacity{}}, Observations: []Observation{}, Activity: []Activity{}}
	d.RecentAgents = []RecentAgent{}
	for _, state := range states {
		d.StateCounts[state] = 0
	}
	for at := w.Start; at.Before(w.End); at = nextBucket(at, period) {
		d.UsageHistory = append(d.UsageHistory, UsagePoint{At: at, Costs: map[string]*float64{}})
		d.Observations = append(d.Observations, Observation{At: at})
	}
	day := dayStart(now)
	for n := 364; n >= 0; n-- {
		d.Activity = append(d.Activity, Activity{Date: day.AddDate(0, 0, -n).In(shanghai).Format("2006-01-02")})
	}
	return d
}
func dayStart(at time.Time) time.Time {
	l := at.In(shanghai)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, shanghai).UTC()
}
func nextBucket(at time.Time, period string) time.Time {
	if period == "today" {
		return at.Add(time.Hour)
	}
	return at.AddDate(0, 0, 1)
}
func bucket(at time.Time, period string) time.Time {
	if period == "today" {
		return at.UTC().Truncate(time.Hour)
	}
	return dayStart(at)
}
func ready() Coverage                  { return Coverage{Status: "ready"} }
func unavailable(note string) Coverage { return Coverage{Status: "unavailable", Note: note} }
func (s *Store) Dashboard(ctx context.Context, userID int, period string, now time.Time) (*Dashboard, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user")
	}
	w, err := makeWindow(period, now)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d := emptyDashboard(period, now, w)
	if err = s.instances(ctx, userID, now, d); err != nil {
		return nil, err
	}
	s.tasks(ctx, userID, now, d)
	s.usage(ctx, userID, d)
	s.readCapacity(ctx, userID, d)
	// Collection failure is isolated from every current metric.
	_ = s.snapshot(ctx, userID, now, d)
	s.observations(ctx, userID, d)
	s.activity(ctx, userID, now, d)
	s.attributionCoverage(ctx, userID, now, d)
	// A slow optional recent source cannot consume the budget for core metrics.
	s.recent(ctx, userID, now, d)
	return d, nil
}

// The same predicate is used by every source, including usage and background samples.
const instanceScope = `i.user_id=? AND i.owner IS NULL AND i.status<>'deleted'
 AND NOT EXISTS(SELECT 1 FROM team_members hidden_m JOIN teams hidden_t ON hidden_t.id=hidden_m.team_id
 WHERE hidden_m.instance_id=i.id AND (hidden_t.resource_scope<>'cm' OR hidden_t.user_id<>i.user_id))`
const stateJoins = ` LEFT JOIN instance_model_activity a ON a.instance_id=i.id AND a.generation=i.runtime_generation
 LEFT JOIN team_members m ON m.id=(SELECT MIN(m2.id) FROM team_members m2 JOIN teams t2 ON t2.id=m2.team_id WHERE m2.instance_id=i.id AND t2.user_id=i.user_id AND t2.resource_scope='cm' AND t2.status<>'deleted')
 LEFT JOIN teams t ON t.id=m.team_id`
const stateExpr = `CASE WHEN i.status IN ('error','failed') THEN 'failed'
 WHEN i.status IN ('creating','starting','restarting','pending') THEN 'starting'
 WHEN i.status='stopped' AND a.stopped_reason='idle_timeout' THEN 'sleeping'
 WHEN i.status IN ('stopped','deleted','deleting') THEN 'offline'
 WHEN i.status='running' AND (EXISTS(SELECT 1 FROM team_tasks tt WHERE tt.target_member_id=m.id AND tt.team_id=t.id AND tt.status IN ('running','dispatched','waiting') AND tt.updated_at>=? AND tt.updated_at<?)
 OR EXISTS(SELECT 1 FROM gateway_runtime_tasks gt WHERE gt.instance_id=i.id AND gt.owner_user_id=i.user_id AND gt.generation=i.runtime_generation AND gt.revoked=FALSE AND gt.finished=FALSE AND gt.updated_at>=? AND gt.updated_at<?)) THEN 'busy'
 WHEN i.status='running' AND m.availability='idle' AND m.last_seen_at>=? AND m.last_seen_at<? THEN 'idle'
 WHEN i.status='running' THEN 'running' ELSE 'unknown' END`

func (s *Store) instances(ctx context.Context, id int, now time.Time, d *Dashboard) error {
	err := s.readInstances(ctx, id, now, d, true)
	if err == nil {
		return nil
	}
	d.Instances = []Agent{}
	d.InstancesTotal = 0
	d.Capacity.AllocatedCPU = 0
	d.Capacity.AllocatedMemoryGB = 0
	d.Capacity.AllocatedStorageGB = 0
	for _, state := range states {
		d.StateCounts[state] = 0
	}
	if err = s.readInstances(ctx, id, now, d, false); err != nil {
		return err
	}
	d.Coverage.Instances = Coverage{Status: "partial", Note: "任务或休眠状态暂不可用，仅显示已确认运行状态"}
	return nil
}
func (s *Store) readInstances(ctx context.Context, id int, now time.Time, d *Dashboard, withEvidence bool) error {
	expression, joins := stateExpr, stateJoins
	args := []any{now.Add(-10 * time.Minute), now, now.Add(-10 * time.Minute), now, now.Add(-10 * time.Minute), now, id}
	if !withEvidence {
		expression = `CASE WHEN i.status IN ('error','failed') THEN 'failed' WHEN i.status IN ('creating','starting','restarting','pending') THEN 'starting' WHEN i.status IN ('stopped','deleted','deleting') THEN 'offline' WHEN i.status='running' THEN 'running' ELSE 'unknown' END`
		joins = strings.Replace(stateJoins, ` LEFT JOIN instance_model_activity a ON a.instance_id=i.id AND a.generation=i.runtime_generation`, "", 1)
		args = []any{id}
	}
	// Aggregate before the 256-row presentation bound.
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT `+expression+`,COUNT(*),COALESCE(SUM(i.cpu_cores),0),COALESCE(SUM(i.memory_gb),0),COALESCE(SUM(i.disk_gb),0) FROM instances i`+joins+` WHERE `+instanceScope+` GROUP BY 1`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var state string
		var n int
		var cpu, mem, disk float64
		if err = rows.Scan(&state, &n, &cpu, &mem, &disk); err != nil {
			rows.Close()
			return err
		}
		d.StateCounts[state] = n
		d.InstancesTotal += n
		d.Capacity.AllocatedCPU += cpu
		d.Capacity.AllocatedMemoryGB += mem
		d.Capacity.AllocatedStorageGB += disk
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = s.sess.SQL().QueryContext(ctx, `SELECT i.id,i.name,i.type,i.instance_mode,i.status,`+expression+`,t.id,COALESCE(t.name,''),i.cpu_cores,i.memory_gb,i.disk_gb,i.created_at,i.updated_at FROM instances i`+joins+` WHERE `+instanceScope+` ORDER BY i.updated_at DESC,i.id DESC LIMIT 256`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a Agent
		if err = rows.Scan(&a.ID, &a.Name, &a.Type, &a.InstanceMode, &a.Status, &a.State, &a.TeamID, &a.TeamName, &a.CPUCores, &a.MemoryGB, &a.StorageGB, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return err
		}
		a.CreatedAt = a.CreatedAt.UTC()
		a.UpdatedAt = a.UpdatedAt.UTC()
		d.Instances = append(d.Instances, a)
	}
	d.InstancesTruncated = d.InstancesTotal > len(d.Instances)
	d.Coverage.Instances = ready()
	return rows.Err()
}
func teamStatus(status string) string {
	switch status {
	case "succeeded", "failed", "pending":
		return status
	case "dispatched", "running", "waiting":
		return "running"
	default:
		return "ended"
	}
}
func boundedText(raw string, limit int) string {
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	r := []rune(strings.TrimSpace(raw))
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}
func taskTitle(raw string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) == nil {
		for _, key := range titleTextFields {
			if text, ok := payload[key].(string); ok && strings.TrimSpace(text) != "" {
				return boundedText(text, 160)
			}
		}
	}
	return "团队任务"
}
func resultExcerpt(raw string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) == nil {
		for _, key := range resultTextFields {
			if text, ok := payload[key].(string); ok && strings.TrimSpace(text) != "" {
				return boundedText(text, 240)
			}
		}
	}
	return ""
}

var titleTextFields = []string{"title", "task", "prompt", "message", "text"}
var resultTextFields = []string{"summary", "resultMarkdown", "result_markdown", "result", "answer", "text", "message"}

// Extract only approved strings inside SQL before bounding their text. Truncating
// raw JSON first can discard every valid field and transfers private payloads.
// Column/field arguments are internal constants, never request values.
func taskTextProjection(column string, fields []string, limit int) string {
	entries := make([]string, 0, len(fields))
	for _, field := range fields {
		value := fmt.Sprintf("JSON_EXTRACT(%s,'$.%s')", column, field)
		entries = append(entries, fmt.Sprintf("'%s',CASE WHEN JSON_TYPE(%s)='STRING' THEN LEFT(JSON_UNQUOTE(%s),%d) END", field, value, value, limit))
	}
	return "CASE WHEN JSON_VALID(" + column + ") THEN JSON_OBJECT(" + strings.Join(entries, ",") + ") ELSE '{}' END"
}

func (s *Store) tasks(ctx context.Context, id int, now time.Time, d *Dashboard) {
	d.Coverage.Tasks = ready()
	complete := 0
	since := now.AddDate(0, 0, -30)
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT tt.id,tt.team_id,m.instance_id,i.name,tt.status,
 `+taskTextProjection("tt.payload_json", titleTextFields, 160)+`,`+taskTextProjection("tt.result_json", resultTextFields, 240)+`,LEFT(COALESCE(tt.error_message,''),240),tt.created_at,tt.updated_at,tt.finished_at
 FROM team_tasks tt JOIN teams t ON t.id=tt.team_id JOIN team_members m ON m.id=tt.target_member_id AND m.team_id=t.id
 JOIN instances i ON i.id=m.instance_id WHERE t.user_id=? AND t.resource_scope='cm' AND t.status<>'deleted' AND `+instanceScope+`
 AND tt.updated_at>=? AND tt.updated_at<? ORDER BY CASE tt.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 WHEN 'dispatched' THEN 1 WHEN 'waiting' THEN 1 WHEN 'succeeded' THEN 2 ELSE 3 END,tt.updated_at DESC,tt.id DESC LIMIT 101`, id, id, since, now)
	if err == nil {
		for rows.Next() {
			var task Task
			var native, payload, result string
			task.Source = "team"
			if err = rows.Scan(&task.ID, &task.TeamID, &task.InstanceID, &task.AgentName, &native, &payload, &result, &task.ErrorMessage, &task.CreatedAt, &task.UpdatedAt, &task.FinishedAt); err != nil {
				break
			}
			task.Key = "team:" + task.ID
			task.Status = teamStatus(native)
			task.Title = taskTitle(payload)
			task.ResultExcerpt = resultExcerpt(result)
			normalizeTask(&task)
			d.Tasks = append(d.Tasks, task)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil {
			complete++
		}
	}
	rows, err = s.sess.SQL().QueryContext(ctx, `SELECT gt.task_key,gt.task_id,gt.instance_id,gt.session_id,i.name,gt.finished,gt.updated_at
 FROM gateway_runtime_tasks gt JOIN instances i ON i.id=gt.instance_id AND gt.owner_user_id=i.user_id AND gt.generation=i.runtime_generation
 WHERE `+instanceScope+` AND gt.revoked=FALSE AND gt.updated_at>=? AND gt.updated_at<?
 AND (gt.finished=TRUE OR gt.updated_at>=?) ORDER BY gt.finished ASC,gt.updated_at DESC LIMIT 101`, id, since, now, now.Add(-10*time.Minute))
	if err == nil {
		for rows.Next() {
			var task Task
			var key string
			var finished bool
			task.Source = "runtime"
			if err = rows.Scan(&key, &task.ID, &task.InstanceID, &task.SessionID, &task.AgentName, &finished, &task.UpdatedAt); err != nil {
				break
			}
			task.Key = "runtime:" + key
			task.Title = "运行时任务"
			task.Status = "running"
			if finished {
				task.Status = "ended"
				task.FinishedAt = &task.UpdatedAt
			}
			task.CreatedAt = task.UpdatedAt
			normalizeTask(&task)
			d.Tasks = append(d.Tasks, task)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil {
			complete++
		}
	}
	// Retain genuine scanned rows from either source, including a valid prefix.
	// Only a fully read source (even empty) establishes available coverage.
	if complete == 1 {
		d.Coverage.Tasks = Coverage{Status: "partial", Note: "团队或运行时任务读取不完整，仅显示已确认记录"}
	} else if complete == 0 {
		d.Coverage.Tasks = unavailable("团队与运行时任务暂不可用，仅显示已确认记录")
	}
	priority := func(status string) int {
		switch status {
		case "failed":
			return 0
		case "running":
			return 1
		case "succeeded":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(d.Tasks, func(i, j int) bool {
		a, b := d.Tasks[i], d.Tasks[j]
		if priority(a.Status) != priority(b.Status) {
			return priority(a.Status) < priority(b.Status)
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
	d.TasksTruncated = len(d.Tasks) > 100
	if d.TasksTruncated {
		d.Tasks = d.Tasks[:100]
	}
	for i := range d.Instances {
		a := &d.Instances[i]
		for _, task := range d.Tasks {
			if task.Status == "running" && task.InstanceID != nil && *task.InstanceID == a.ID && a.State == "busy" {
				a.CurrentTaskID = task.ID
				a.CurrentTaskTitle = task.Title
				a.SessionID = task.SessionID
				break
			}
		}
	}
}
func normalizeTask(task *Task) {
	task.CreatedAt = task.CreatedAt.UTC()
	task.UpdatedAt = task.UpdatedAt.UTC()
	if task.FinishedAt != nil {
		v := task.FinishedAt.UTC()
		task.FinishedAt = &v
	}
}
func (s *Store) readCapacity(ctx context.Context, id int, d *Dashboard) {
	d.Coverage.Capacity = Coverage{Status: "partial", Note: "配额为已分配资源；可用运行容量暂不可用"}
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT max_instances,max_cpu_cores,max_memory_gb,max_storage_gb FROM user_quotas WHERE user_id=?`, id)
	if err == nil {
		err = row.Scan(&d.Capacity.MaxInstances, &d.Capacity.MaxCPU, &d.Capacity.MaxMemoryGB, &d.Capacity.MaxStorageGB)
	}
	if err != nil && err != sql.ErrNoRows {
		d.Coverage.Capacity.Note = "配额与可用运行容量暂不可用"
	}
	if s.capacity == nil {
		return
	}
	available, e := s.capacity(ctx)
	if e != nil {
		return
	}
	total := 0
	for kind, n := range available {
		value := n
		d.Capacity.Runtimes = append(d.Capacity.Runtimes, RuntimeCapacity{Type: kind, Available: &value})
		total += n
	}
	sort.Slice(d.Capacity.Runtimes, func(i, j int) bool { return d.Capacity.Runtimes[i].Type < d.Capacity.Runtimes[j].Type })
	d.Capacity.EligibleSlots = &total
	if err == nil {
		d.Coverage.Capacity = ready()
	} else {
		d.Coverage.Capacity = Coverage{Status: "partial", Note: "个人配额暂不可用"}
	}
}
