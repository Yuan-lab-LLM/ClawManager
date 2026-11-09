package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"strings"
	"time"
)

const growthRule = "growth-v1"

type GrowthTask struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Period   string `json:"period"`
	Metric   string `json:"metric"`
	Target   uint64 `json:"target"`
	Progress uint64 `json:"progress"`
	XP       int64  `json:"xp"`
	Awarded  bool   `json:"awarded"`
	Reward   string `json:"reward,omitempty"`
}
type XPEntry struct {
	Name string    `json:"name"`
	XP   int64     `json:"xp"`
	At   time.Time `json:"at"`
}
type Growth struct {
	Snapshot   *SnapshotState `json:"snapshot,omitempty"`
	AsOf       time.Time      `json:"as_of"`
	XP         int64          `json:"xp"`
	Level      int            `json:"level"`
	LevelStart int64          `json:"level_start"`
	NextLevel  int64          `json:"next_level"`
	StartedAt  *time.Time     `json:"started_at"`
	Tasks      []GrowthTask   `json:"tasks"`
	Ledger     []XPEntry      `json:"ledger"`
	Days       []struct {
		Date string `json:"date"`
		XP   int64  `json:"xp"`
	} `json:"days"`
	Rule            string        `json:"rule"`
	Note            string        `json:"note"`
	OpenLevel       int           `json:"open_level"`
	PlannedLevel    int           `json:"-"`
	DailyLimit      int64         `json:"daily_limit"`
	TodayXP         int64         `json:"today_xp"`
	EffectiveAt     time.Time     `json:"effective_at"`
	UpcomingTasks   []GrowthTask  `json:"upcoming_tasks,omitempty"`
	Ranking         *TokenRanking `json:"ranking,omitempty"`
	LevelThresholds []int64       `json:"level_thresholds"`
}

func growthTasks() []GrowthTask {
	return []GrowthTask{
		{Code: "login-day", Name: "今日首次访问", Period: "day", Metric: "login", Target: 1, XP: 5},
		{Code: "question-1", Name: "完成 1 次有效提问", Period: "day", Metric: "questions", Target: 1, XP: 2},
		{Code: "question-3", Name: "完成 3 次有效提问", Period: "day", Metric: "questions", Target: 3, XP: 4},
		{Code: "question-10", Name: "完成 10 次有效提问", Period: "day", Metric: "questions", Target: 10, XP: 8},
		{Code: "work-15", Name: "前台活跃使用 15 分钟", Period: "day", Metric: "seconds", Target: 900, XP: 5},
		{Code: "work-30", Name: "前台活跃使用 30 分钟", Period: "day", Metric: "seconds", Target: 1800, XP: 10},
		{Code: "work-60", Name: "前台活跃使用 60 分钟", Period: "day", Metric: "seconds", Target: 3600, XP: 15},
		{Code: "login-3", Name: "累计访问 3 天", Period: "all", Metric: "login", Target: 3, XP: 10},
		{Code: "login-7", Name: "累计访问 7 天", Period: "all", Metric: "login", Target: 7, XP: 25},
		{Code: "login-30", Name: "累计访问 30 天", Period: "all", Metric: "login", Target: 30, XP: 100},
		{Code: "questions-10", Name: "累计有效提问 10 次", Period: "all", Metric: "questions", Target: 10, XP: 10},
		{Code: "questions-100", Name: "累计有效提问 100 次", Period: "all", Metric: "questions", Target: 100, XP: 50},
		{Code: "questions-500", Name: "累计有效提问 500 次", Period: "all", Metric: "questions", Target: 500, XP: 200},
		{Code: "work-hour", Name: "累计前台活跃 1 小时", Period: "all", Metric: "seconds", Target: 3600, XP: 20},
		{Code: "work-ten-hours", Name: "累计前台活跃 10 小时", Period: "all", Metric: "seconds", Target: 36000, XP: 100},
		{Code: "work-fifty-hours", Name: "累计前台活跃 50 小时", Period: "all", Metric: "seconds", Target: 180000, XP: 300},
	}
}
func levelForXP(xp int64) (int, int64, int64) {
	return levelForRuleXP(xp, growthRule)
}

func levelForRuleXP(xp int64, rule string) (int, int64, int64) {
	threshold := levelThreshold
	if rule == growthRuleV4 {
		threshold = levelThresholdV4
	}
	level := 1
	for level < openGrowthLevel && threshold(level+1) <= xp {
		level++
	}
	next := threshold(level + 1)
	if level == openGrowthLevel {
		next = threshold(level)
	}
	return level, threshold(level), next
}
func normalizedOwner(owner string) (string, error) {
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" || len(owner) > 128 {
		return "", fmt.Errorf("verified owner required")
	}
	return owner, nil
}

// Growth is a portal-owner projection (scope 0), matching the existing verified
// IEI owner boundary. It is deliberately separate from caller account profiles.
func (s *Store) VisitGrowth(ctx context.Context, owner string, now time.Time) error {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := now.In(loc).Format("2006-01-02")
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='login_day' AND record_key=?`, Key("login", owner, day))
	if err != nil {
		return err
	}
	var exists int
	if err = row.Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	err = s.Session.TxContext(ctx, func(tx db.Session) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,occurred_at,rule_version) VALUES ('growth_profile',?,0,?,'active',0,?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, Key("growth", owner), owner, now.UTC(), growthRule)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,occurred_at) VALUES ('login_day',?,0,?,'confirmed',?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, Key("login", owner, day), owner, day, now.UTC())
		if err == nil {
			err = markUsageDay(ctx, tx, owner, now)
		}
		return err
	}, nil)
	if err != nil {
		return err
	}
	return s.ReconcileGrowth(ctx, owner, now)
}

func (s *Store) ReconcileGrowth(ctx context.Context, owner string, now time.Time) error {
	effective, err := s.growthEffectiveAt(ctx, now)
	if err != nil {
		return err
	}
	v3, err := s.growthV3EffectiveAt(ctx, now)
	if err != nil {
		return err
	}
	if err := s.reconcileGrowthAt(ctx, owner, now, now, false, effective, v3); err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	pending, err := s.pendingDays(ctx, strings.ToLower(strings.TrimSpace(owner)), 100, now)
	if err != nil {
		return err
	}
	dates := []string{}
	seen := map[string]bool{}
	for _, d := range pending {
		date := d.date.Format("2006-01-02")
		if date < now.In(loc).Format("2006-01-02") && !seen[date] {
			dates = append(dates, date)
			seen[date] = true
		}
	}
	for _, date := range dates {
		day, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return err
		}
		if err = s.reconcileGrowthAt(ctx, owner, day.AddDate(0, 0, 1).Add(-time.Microsecond), now, true, effective, v3); err != nil {
			return err
		}
	}
	// Historical replay can complete an earlier full-task day. Refresh cumulative
	// milestones only after all replayed daily rewards have settled.
	if len(dates) > 0 && !now.Before(effective) {
		return s.reconcileGrowthAt(ctx, owner, now, now, false, effective, v3)
	}
	return nil
}

func (s *Store) reconcileGrowthAt(ctx context.Context, owner string, now, rewardAt time.Time, historical bool, effective time.Time, v3 ...time.Time) error {
	return s.reconcileGrowthSelected(ctx, owner, now, rewardAt, historical, false, effective, v3...)
}

func (s *Store) reconcileGrowthSelected(ctx context.Context, owner string, now, rewardAt time.Time, historical, visitOnly bool, effective time.Time, v3 ...time.Time) error {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := now.In(loc).Format("2006-01-02")
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	rule, tasks := growthTasksAt(now, effective, v3...)
	v4, err := s.policy(ctx, growthRuleV4, now, false)
	if err != nil {
		return err
	}
	if !now.Before(v4) {
		rule, tasks = growthRuleV4, growthTasksV4()
	}
	if visitOnly {
		selected := []GrowthTask{}
		for _, task := range tasks {
			if task.Period == "day" && task.Metric == "login" {
				selected = append(selected, task)
			}
		}
		tasks = selected
	}
	if !visitOnly {
		if err = s.ensureDefinitions(ctx, rule, tasks); err != nil {
			return err
		}
	}
	changed := false
	err = s.Session.TxContext(ctx, func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT occurred_at FROM northbound_owner_records WHERE record_type='growth_profile' AND record_key=? FOR UPDATE`, Key("growth", owner))
		if err != nil {
			return err
		}
		var started time.Time
		if err = row.Scan(&started); err == sql.ErrNoRows {
			return nil
		} else if err != nil {
			return err
		}
		progressCache := make(map[string]uint64)
		priorCategory := map[string]int64{}
		categoryBudget := map[string]int64{}
		for _, task := range tasks {
			if historical && task.Period != "day" {
				continue
			}
			start := started
			if rule == growthRuleV4 && task.Period == "all" && task.Metric == "tokens" {
				start = time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			if rule != growthRule && rule != growthRuleV4 && task.Period == "day" && effective.After(start) {
				start = effective
			}
			if task.Period == "day" && today.After(start) {
				start = today
			}
			if rule == growthRuleV4 && task.Period == "day" {
				start = today
			}
			awardXP := task.XP
			if rule == growthRuleV4 && task.Period == "day" {
				prior, ok := priorCategory[task.Metric]
				if !ok {
					pattern := map[string]string{"login": "login-day", "questions": "question-%", "tokens": "tokens-%"}[task.Metric]
					r, e := tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(xp_value),0) FROM northbound_owner_records WHERE record_type='xp_event' AND owner_normalized=? AND business_date=? AND definition_version<>? AND task_code LIKE ?`, owner, day, growthRuleV4, pattern)
					if e != nil {
						return e
					}
					if e = r.Scan(&prior); e != nil {
						return e
					}
					priorCategory[task.Metric] = prior
				}
				credit := prior - categoryBudget[task.Metric]
				if credit < 0 {
					credit = 0
				}
				if credit > task.XP {
					credit = task.XP
				}
				awardXP -= credit
				categoryBudget[task.Metric] += task.XP
			}
			var progress uint64
			query := `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='question' AND owner_normalized=? AND occurred_at>=? AND occurred_at<=?`
			if task.Metric == "login" {
				query = `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='login_day' AND owner_normalized=? AND occurred_at>=? AND occurred_at<=?`
			}
			if task.Metric == "seconds" {
				query = `SELECT COALESCE(SUM(active_work_seconds),0) FROM northbound_owner_records WHERE record_type IN ('activity_tick','activity_window') AND owner_normalized=? AND occurred_at>=? AND occurred_at<=?`
			}
			if task.Metric == "tokens" {
				query = `SELECT COALESCE(SUM(total_tokens),0) FROM northbound_owner_records WHERE record_type='invocation' AND owner_normalized=? AND occurred_at>=? AND occurred_at<=?`
			}
			if task.Metric == "full_days" {
				query = `SELECT COUNT(*) FROM (SELECT business_date FROM northbound_owner_records WHERE record_type='xp_event' AND owner_normalized=? AND business_date>=DATE(DATE_ADD(?,INTERVAL 8 HOUR)) AND business_date<=DATE(DATE_ADD(?,INTERVAL 8 HOUR)) AND definition_version IN ('growth-v2','growth-v3') GROUP BY business_date HAVING SUM(xp_value)=100) completed_days`
			}
			cacheKey := task.Period + ":" + task.Metric
			if cached, ok := progressCache[cacheKey]; ok {
				progress = cached
			} else {
				query, err = s.factSQL(ctx, query)
				if err != nil {
					return err
				}
				row, err = tx.SQL().QueryRowContext(ctx, query, owner, start.UTC(), now.UTC())
				if err != nil {
					return err
				}
				if err = row.Scan(&progress); err != nil {
					return err
				}
				progressCache[cacheKey] = progress
			}
			period := "all"
			if progress < task.Target {
				continue
			}
			if task.Period == "day" {
				period = day
			}
			key := Key("task", owner, rule, task.Code, period)
			prior, e := tx.SQL().QueryRowContext(ctx, `SELECT progress_value,status FROM northbound_owner_records WHERE record_type='task_progress' AND record_key=?`, key)
			if e != nil {
				return e
			}
			var oldProgress uint64
			var oldStatus string
			e = prior.Scan(&oldProgress, &oldStatus)
			if e != nil && e != sql.ErrNoRows {
				return e
			}
			if e == nil && oldStatus == "awarded" {
				continue
			}
			if e == sql.ErrNoRows || progress > oldProgress {
				_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,task_code,period_type,business_date,progress_value,target_value,definition_version) VALUES ('task_progress',?,0,?,'tracking',?,?,?,?,?,?) ON DUPLICATE KEY UPDATE progress_value=GREATEST(COALESCE(progress_value,0),VALUES(progress_value)),updated_at=CURRENT_TIMESTAMP(6)`, key, owner, task.Code, task.Period, day, progress, task.Target, rule)
				if err != nil {
					return err
				}
			}
			if progress < task.Target {
				continue
			}
			if awardXP == 0 {
				changed = true
				// A persisted collection unlock, not a zero-XP ledger reward.
				_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='awarded',completed_at=COALESCE(completed_at,?),rewarded_at=COALESCE(rewarded_at,?) WHERE record_type='task_progress' AND record_key=?`, now.UTC(), rewardAt.UTC(), key)
				if err != nil {
					return err
				}
				continue
			}
			row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='xp_event' AND record_key=?`, key)
			if err != nil {
				return err
			}
			var exists int
			if err = row.Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				continue
			}
			payload, _ := json.Marshal(map[string]string{"name": task.Name})
			changed = true
			_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,task_code,xp_value,occurred_at,business_date,definition_version,payload) VALUES ('xp_event',?,0,?,'awarded',?,?,?,?,?,?)`, key, owner, task.Code, awardXP, rewardAt.UTC(), day, rule, string(payload))
			if err != nil {
				return err
			}
			_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET xp_value=xp_value+?,version=version+1 WHERE record_type='growth_profile' AND record_key=?`, awardXP, Key("growth", owner))
			if err != nil {
				return err
			}
			_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='awarded',completed_at=?,rewarded_at=? WHERE record_type='task_progress' AND record_key=?`, now.UTC(), rewardAt.UTC(), key)
			if err != nil {
				return err
			}
		}
		return nil
	}, nil)
	if err == nil && changed {
		s.InvalidateOwner(owner)
	}
	return err
}
func (s *Store) Growth(ctx context.Context, owner string, now time.Time) (*Growth, error) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	effective, err := s.policy(ctx, growthRuleV2, now, false)
	if err != nil {
		return nil, err
	}
	v3, err := s.policy(ctx, growthRuleV3, now, false)
	if err != nil {
		return nil, err
	}
	ranking, err := s.TokenRanking(ctx, owner, now)
	if err != nil {
		return nil, err
	}
	var result *Growth
	err = s.Session.TxContext(ctx, func(tx db.Session) error {
		reader := &Store{Session: tx}
		var e error
		result, e = reader.growthSnapshot(ctx, owner, now, effective, v3, ranking)
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

func (s *Store) growthSnapshot(ctx context.Context, owner string, now, effective, v3 time.Time, ranking *TokenRanking) (*Growth, error) {
	v4, err := s.policy(ctx, growthRuleV4, now, false)
	if err != nil {
		return nil, err
	}
	rule, tasks := growthTasksAt(now, effective, v3, v4)
	g := &Growth{Tasks: tasks, Ledger: []XPEntry{}, Rule: rule, OpenLevel: openGrowthLevel, PlannedLevel: plannedGrowthLevel, DailyLimit: 100, EffectiveAt: effective, Note: "新规则按北京时间零点切换；旧 XP 保留，不追溯补发启用前奖励。访问 10 XP、有效提问 50 XP、Token 用量 40 XP，每日最多 100 XP，阶梯分别累加且自动发放。仅统计平台已采集且去重的数据；迟到事实计回发生日。累计里程碑解锁收藏章，不额外发 XP。新用户每日满额第 177 天达到 Lv.50（包含周末）。当前开放 50 级，达到开放上限后经验仍会累计。积分不关联付费及访问权限。"}
	if rule == growthRule {
		g.UpcomingTasks = growthTasksV2()
		g.DailyLimit = 49
	}
	if now.Before(v3) {
		g.UpcomingTasks = growthTasksV3()
		g.EffectiveAt = v3
	}
	if rule == growthRuleV4 {
		g.UpcomingTasks = nil
		g.EffectiveAt = v4
		g.DailyLimit = 105
		g.Note = "每日六项任务最多获得 105 XP；累计访问、有效提问、Token 用量三个类别共提供 8,100 XP。任务达标后自动入账，已获经验保留。Lv.50 需要累计 27,000 XP；从零开始，每日满额且累计任务全部达成，最快第 180 个自然日达到。仅统计平台已采集且去重的数据；有效提问从统计启用后开始计算。积分不关联付费及访问权限。"
	}
	for level := 1; level <= openGrowthLevel; level++ {
		threshold := levelThreshold(level)
		if rule == growthRuleV4 {
			threshold = levelThresholdV4(level)
		}
		g.LevelThresholds = append(g.LevelThresholds, threshold)
	}
	g.Ranking = ranking
	g.AsOf = now
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT xp_value,occurred_at FROM northbound_owner_records WHERE record_type='growth_profile' AND record_key=?`, Key("growth", owner))
	if err != nil {
		return nil, err
	}
	var started time.Time
	if err = row.Scan(&g.XP, &started); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		g.StartedAt = &started
	}
	g.Level, g.LevelStart, g.NextLevel = levelForRuleXP(g.XP, rule)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := now.In(loc).Format("2006-01-02")
	counts := map[string]uint64{}
	if g.StartedAt != nil {
		daily := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
		if rule != growthRuleV4 && started.After(daily) {
			daily = started
		}
		if rule != growthRule && rule != growthRuleV4 && effective.After(daily) {
			daily = effective
		}
		counts, err = s.taskCounts(ctx, owner, started, daily, now)
		if err != nil {
			return nil, err
		}
	}
	keys := make([]any, 0, len(g.Tasks))
	byKey := map[string]*GrowthTask{}
	for i := range g.Tasks {
		task := &g.Tasks[i]
		task.Progress = counts[task.Period+":"+task.Metric]
		period := "all"
		if task.Period == "day" {
			period = day
		}
		key := Key("task", owner, rule, task.Code, period)
		keys = append(keys, key)
		byKey[key] = task
	}
	if len(keys) > 0 {
		progressRows, e := s.Session.SQL().QueryContext(ctx, `SELECT record_key,progress_value,status FROM northbound_owner_records WHERE record_type='task_progress' AND record_key IN (`+strings.TrimRight(strings.Repeat("?,", len(keys)), ",")+`)`, keys...)
		if e != nil {
			return nil, e
		}
		for progressRows.Next() {
			var key, status string
			var progress uint64
			if e = progressRows.Scan(&key, &progress, &status); e != nil {
				progressRows.Close()
				return nil, e
			}
			if task := byKey[key]; task != nil {
				task.Awarded = status == "awarded"
			}
		}
		e = progressRows.Err()
		progressRows.Close()
		if e != nil {
			return nil, e
		}
	}
	rows, err := s.Session.SQL().QueryContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.name')),task_code),xp_value,occurred_at FROM northbound_owner_records WHERE record_type='xp_event' AND owner_normalized=? ORDER BY occurred_at DESC,id DESC LIMIT 100`, owner)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e XPEntry
		if err = rows.Scan(&e.Name, &e.XP, &e.At); err != nil {
			rows.Close()
			return nil, err
		}
		g.Ledger = append(g.Ledger, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.Session.SQL().QueryContext(ctx, `SELECT business_date,SUM(xp_value) FROM northbound_owner_records WHERE record_type='xp_event' AND owner_normalized=? AND business_date>=? GROUP BY business_date ORDER BY business_date`, owner, now.In(loc).AddDate(0, 0, -29).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var date time.Time
		var xp int64
		if err = rows.Scan(&date, &xp); err != nil {
			return nil, err
		}
		g.Days = append(g.Days, struct {
			Date string `json:"date"`
			XP   int64  `json:"xp"`
		}{date.Format("2006-01-02"), xp})
		if date.Format("2006-01-02") == day {
			g.TodayXP = xp
		}
	}
	return g, rows.Err()
}
