package ownerrecords

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Metrics struct {
	PromptTokens     uint64  `json:"prompt_tokens"`
	CompletionTokens uint64  `json:"completion_tokens"`
	TotalTokens      uint64  `json:"total_tokens"`
	Sessions         uint64  `json:"sessions"`
	Questions        *uint64 `json:"questions"`
	WorkdayStreak    *uint64 `json:"workday_streak"`
}
type Day struct {
	Date   string `json:"date"`
	Tokens uint64 `json:"tokens"`
}
type Coverage struct {
	QuestionsSupported   bool   `json:"questions_supported"`
	Partial              bool   `json:"partial"`
	Description          string `json:"description"`
	ReadyHistories       uint64 `json:"ready_histories"`
	UnavailableHistories uint64 `json:"unavailable_histories"`
}
type Dashboard struct {
	Growth          *Growth                   `json:"growth,omitempty"`
	Snapshot        *SnapshotState            `json:"snapshot,omitempty"`
	RecentQuestions map[uint64]RecentQuestion `json:"recent_questions,omitempty"`
	Insights        *Insights                 `json:"insights,omitempty"`
	Summary         Metrics                   `json:"summary"`
	Trend           []Day                     `json:"trend"`
	LastActivity    map[uint64]string         `json:"last_activity"`
	Coverage        Coverage                  `json:"coverage"`
	Timezone        string                    `json:"timezone"`
	Range           string                    `json:"range"`
}

// Dashboard is read-only. A nil scope is reserved for a server-verified IEI
// identity, whose existing authorization covers its owner across caller accounts.
// The public northbound API must always pass the authenticated account scope.
func (s *Store) Dashboard(ctx context.Context, owner string, scope *uint64, period string, now time.Time) (*Dashboard, error) {
	if !s.Available() {
		return nil, fmt.Errorf("owner analytics unavailable")
	}
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" {
		return nil, fmt.Errorf("verified owner required")
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return nil, err
	}
	now = now.In(loc)
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	var begin time.Time
	switch period {
	case "7d":
		begin = end.AddDate(0, 0, -7)
	case "30d":
		begin = end.AddDate(0, 0, -30)
	case "month":
		begin = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	default:
		return nil, fmt.Errorf("range must be 7d, 30d or month")
	}
	where := "owner_normalized=?"
	args := []any{owner}
	if scope != nil {
		if *scope == 0 {
			return nil, fmt.Errorf("account scope required")
		}
		where += " AND scope_id=?"
		args = append(args, *scope)
	}
	result := &Dashboard{Trend: []Day{}, LastActivity: map[uint64]string{}, Timezone: "Asia/Shanghai", Range: period, Coverage: Coverage{Partial: true, Description: "Token 仅统计平台已采集调用；有效提问按原生消息标识去重，只计有明确成功回复的人工提问。无法识别、已删除的历史不推测补数。连续工作日为北京时间周一至周五。"}}
	var questions uint64
	query := `SELECT COALESCE(SUM(prompt_tokens),0),COALESCE(SUM(completion_tokens),0),COALESCE(SUM(total_tokens),0),COALESCE(SUM(session_count),0),COALESCE(SUM(question_count),0) FROM northbound_owner_records WHERE record_type='profile' AND ` + where
	epoch, e := s.analyticsEpoch(ctx)
	if e != nil {
		return nil, e
	}
	if epoch != nil {
		query = `SELECT COALESCE(SUM(prompt_tokens),0),COALESCE(SUM(completion_tokens),0),COALESCE(SUM(total_tokens),0),COALESCE(SUM(record_type='source_session'),0),COALESCE(SUM(record_type='question_v4'),0) FROM northbound_owner_records WHERE record_type IN ('source_usage','source_session','question_v4') AND ` + where
	}
	row, err := s.factRow(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&result.Summary.PromptTokens, &result.Summary.CompletionTokens, &result.Summary.TotalTokens, &result.Summary.Sessions, &questions); err != nil {
		return nil, err
	}
	row, err = s.factRow(ctx, `SELECT COALESCE(SUM(status='ready'),0),COALESCE(SUM(status<>'ready'),0) FROM northbound_owner_records WHERE record_type='history_scan' AND `+where, args...)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&result.Coverage.ReadyHistories, &result.Coverage.UnavailableHistories); err != nil {
		return nil, err
	}
	if epoch != nil || result.Coverage.ReadyHistories > 0 || questions > 0 {
		result.Summary.Questions = &questions
		result.Coverage.QuestionsSupported = true
	}
	trendArgs := append(append([]any{}, args...), begin.UTC(), end.UTC())
	rows, err := s.factRows(ctx, `SELECT DATE_FORMAT(DATE_ADD(occurred_at,INTERVAL 8 HOUR),'%Y-%m-%d'),SUM(total_tokens) FROM northbound_owner_records WHERE record_type='invocation' AND `+where+` AND occurred_at>=? AND occurred_at<? GROUP BY DATE_FORMAT(DATE_ADD(occurred_at,INTERVAL 8 HOUR),'%Y-%m-%d')`, trendArgs...)
	if err != nil {
		return nil, err
	}
	daily := map[string]uint64{}
	for rows.Next() {
		var date string
		var value uint64
		if err = rows.Scan(&date, &value); err != nil {
			rows.Close()
			return nil, err
		}
		daily[date] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for day := begin; day.Before(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		result.Trend = append(result.Trend, Day{Date: date, Tokens: daily[date]})
	}
	rows, err = s.factRows(ctx, `SELECT instance_id,MAX(occurred_at) FROM northbound_owner_records WHERE record_type='question' AND `+where+` GROUP BY instance_id`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uint64
		var at time.Time
		if err = rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, err
		}
		result.LastActivity[id] = at.UTC().Format(time.RFC3339)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if result.Coverage.QuestionsSupported {
		rows, err = s.factRows(ctx, `SELECT DISTINCT DATE_FORMAT(DATE_ADD(occurred_at,INTERVAL 8 HOUR),'%Y-%m-%d') AS day FROM northbound_owner_records WHERE record_type='question' AND `+where+` ORDER BY day DESC LIMIT 10000`, args...)
		if err != nil {
			return nil, err
		}
		var dates []string
		for rows.Next() {
			var day string
			if err = rows.Scan(&day); err != nil {
				rows.Close()
				return nil, err
			}
			dates = append(dates, day)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		streak := uint64(WorkdayStreak(dates, now))
		result.Summary.WorkdayStreak = &streak
	}
	result.Insights, err = s.Insights(ctx, owner, scope, now)
	if err != nil {
		return nil, err
	}
	// Optional presentation data fails closed independently of usage statistics.
	// Public owners:usage:read grants metrics, not conversation content. Only the
	// existing server-verified portal identity (nil scope) receives excerpts.
	if scope == nil {
		result.RecentQuestions, _ = s.recentQuestions(ctx, owner, scope, now)
	}
	return result, nil
}
