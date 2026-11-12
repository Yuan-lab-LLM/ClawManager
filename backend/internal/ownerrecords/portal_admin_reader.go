package ownerrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const portalUserQueryBudget = 5 * time.Second

// Only scalar sort keys are evaluated globally. Rich snapshot details are read
// after paging; visits/calendar previews never load another user's task JSON.
func (s *Store) portalUserKeys(ctx context.Context, search, order string, offset, limit int, after string) ([]PortalUser, error) {
	query := `SELECT g.owner_normalized,COALESCE(g.xp_value,0),COALESCE(g.rule_version,'') FROM northbound_owner_records g`
	sortKey := "g.xp_value"
	if order == "tokens" || order == "tasks" {
		query += ` LEFT JOIN northbound_owner_records p ON p.record_type='owner_snapshot' AND p.scope_id=0 AND p.status='published' AND p.owner_normalized=g.owner_normalized`
		if order == "tokens" {
			sortKey = `COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(p.payload,'$.dashboard.summary.total_tokens')) AS UNSIGNED),0)`
		} else {
			sortKey = `(SELECT COUNT(*) FROM JSON_TABLE(COALESCE(JSON_EXTRACT(p.payload,'$.growth.tasks'),JSON_ARRAY()),'$[*]' COLUMNS(task_period VARCHAR(16) PATH '$.period', awarded TINYINT PATH '$.awarded')) jt WHERE jt.task_period='day' AND jt.awarded=1)`
		}
	}
	query += ` WHERE g.record_type='growth_profile' AND g.owner_normalized IS NOT NULL AND g.owner_normalized<>'' AND LOCATE(?,g.owner_normalized)>0`
	args := []any{strings.ToLower(search)}
	if after != "" {
		query += ` AND g.owner_normalized>?`
		args = append(args, after)
	}
	if order == "owner" {
		query += ` ORDER BY g.owner_normalized`
	} else {
		query += ` ORDER BY ` + sortKey + ` DESC,g.owner_normalized`
	}
	query += ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	q, cancel := context.WithTimeout(ctx, portalUserQueryBudget)
	defer cancel()
	rows, err := s.Session.SQL().QueryContext(q, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortalUser{}
	for rows.Next() {
		var u PortalUser
		var rule string
		if err = rows.Scan(&u.Owner, &u.XP, &rule); err != nil {
			return nil, err
		}
		u.Level, _, _ = levelForRuleXP(u.XP, rule)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) portalUserVisits(ctx context.Context, users []PortalUser, heads map[int]CalendarVersion, now time.Time) error {
	for start := 0; start < len(users); start += 128 {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+128, len(users))
		marks := []string{}
		owners := []any{}
		dates := map[string][]string{}
		for _, u := range users[start:end] {
			marks = append(marks, "?")
			owners = append(owners, u.Owner)
		}
		lastOwner, lastDate := "", "1000-01-01"
		for {
			query := `SELECT DISTINCT owner_normalized,DATE_FORMAT(business_date,'%Y-%m-%d') AS visit_date FROM northbound_owner_records WHERE record_type='login_day' AND business_date IS NOT NULL AND owner_normalized IN (` + strings.Join(marks, ",") + `) AND (owner_normalized>? OR (owner_normalized=? AND business_date>?)) ORDER BY owner_normalized,visit_date LIMIT 2000`
			args := append(append([]any{}, owners...), lastOwner, lastOwner, lastDate)
			q, cancel := context.WithTimeout(ctx, portalUserQueryBudget)
			rows, err := s.Session.SQL().QueryContext(q, query, args...)
			if err != nil {
				cancel()
				return err
			}
			n := 0
			for rows.Next() {
				var owner, date string
				if err = rows.Scan(&owner, &date); err != nil {
					rows.Close()
					cancel()
					return err
				}
				dates[owner] = append(dates[owner], date)
				lastOwner, lastDate = owner, date
				n++
			}
			err = rows.Err()
			rows.Close()
			cancel()
			if err != nil {
				return err
			}
			if n < 2000 {
				break
			}
		}
		for j := start; j < end; j++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			users[j].Visits = calculateVisits(dates[users[j].Owner], heads, now)
			users[j].Visits.Days = nil
		}
	}
	return nil
}

// Used by calendar previews and global visit ordering: keyset pages of small
// profile rows, no global user-count rejection and no rich snapshot transfer.
func (s *Store) portalUsers(ctx context.Context, heads map[int]CalendarVersion, now time.Time) ([]PortalUser, error) {
	users := []PortalUser{}
	after := ""
	for {
		part, err := s.portalUserKeys(ctx, "", "owner", 0, 256, after)
		if err != nil {
			return nil, err
		}
		users = append(users, part...)
		if len(part) < 256 {
			break
		}
		after = part[len(part)-1].Owner
	}
	if err := s.portalUserVisits(ctx, users, heads, now); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *Store) portalUserDetails(ctx context.Context, users []PortalUser) error {
	if len(users) == 0 {
		return nil
	}
	marks := []string{}
	args := []any{}
	index := map[string]int{}
	for j, u := range users {
		marks = append(marks, "?")
		args = append(args, Key("owner-published", u.Owner))
		index[u.Owner] = j
	}
	q, cancel := context.WithTimeout(ctx, portalUserQueryBudget)
	defer cancel()
	rows, err := s.Session.SQL().QueryContext(q, `SELECT owner_normalized,COALESCE(JSON_EXTRACT(payload,'$.dashboard.summary'),'null'),COALESCE(JSON_EXTRACT(payload,'$.growth.tasks'),'[]'),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.growth.as_of')),'') FROM northbound_owner_records WHERE record_type='owner_snapshot' AND record_key IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var owner, at string
		var summary, tasks []byte
		if err = rows.Scan(&owner, &summary, &tasks, &at); err != nil {
			return err
		}
		j, ok := index[owner]
		if !ok {
			continue
		}
		var m Metrics
		var ts []GrowthTask
		if err = json.Unmarshal(summary, &m); err != nil {
			return err
		}
		if err = json.Unmarshal(tasks, &ts); err != nil {
			return err
		}
		u := &users[j]
		u.Ready = string(summary) != "null"
		u.Tokens = m.TotalTokens
		u.Questions = m.Questions
		u.Sessions = m.Sessions
		u.SnapshotAt = at
		for _, t := range ts {
			if t.Period == "day" {
				u.TasksTotal++
				if t.Awarded {
					u.TasksDone++
				}
			}
		}
	}
	return rows.Err()
}

func (s *Store) PortalUsers(ctx context.Context, search, order string, page int, now time.Time) (PortalUsers, error) {
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return PortalUsers{}, fmt.Errorf("分页范围过大，请缩小搜索条件")
	}
	heads, err := s.calendarHeads(ctx)
	if err != nil {
		return PortalUsers{}, err
	}
	var users []PortalUser
	var total int
	if order == "streak" || order == "visits" {
		users, err = s.portalUsers(ctx, heads, now)
		if err != nil {
			return PortalUsers{}, err
		}
		filtered := users[:0]
		for _, u := range users {
			if strings.Contains(u.Owner, strings.ToLower(search)) {
				filtered = append(filtered, u)
			}
		}
		users = filtered
		sort.Slice(users, func(i, j int) bool {
			a, b := users[i].Visits.Streak, users[j].Visits.Streak
			if order == "visits" {
				a, b = users[i].Visits.Total, users[j].Visits.Total
			}
			if a != b {
				return a > b
			}
			return users[i].Owner < users[j].Owner
		})
		total = len(users)
		start := min((page-1)*20, total)
		users = users[start:min(start+20, total)]
	} else {
		q, cancel := context.WithTimeout(ctx, portalUserQueryBudget)
		row, e := s.Session.SQL().QueryRowContext(q, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='growth_profile' AND owner_normalized IS NOT NULL AND owner_normalized<>'' AND LOCATE(?,owner_normalized)>0`, strings.ToLower(search))
		if e == nil {
			e = row.Scan(&total)
		}
		cancel()
		if e != nil {
			return PortalUsers{}, e
		}
		users, err = s.portalUserKeys(ctx, search, order, (page-1)*20, 20, "")
		if err != nil {
			return PortalUsers{}, err
		}
		if err = s.portalUserVisits(ctx, users, heads, now); err != nil {
			return PortalUsers{}, err
		}
	}
	if err = s.portalUserDetails(ctx, users); err != nil {
		return PortalUsers{}, err
	}
	return PortalUsers{Users: users, Total: total, Page: page, AsOf: now.UTC()}, nil
}
