package workspacedashboard

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Recent use is recorded activity, never instance lifecycle/heartbeat time.
// Each source contributes at most seven grouped Agents; one extra detects the cap.
func (s *Store) recent(ctx context.Context, id int, now time.Time, d *Dashboard) {
	type use struct {
		id   int
		at   time.Time
		kind string
	}
	latest := map[int]use{}
	complete := 0
	for _, source := range []struct{ kind, from, at, where string }{
		{"model", "model_invocations e JOIN instances i ON i.id=e.instance_id AND e.user_id=i.user_id", "e.created_at", "e.status<>'blocked'"},
		{"chat", "chat_sessions e JOIN instances i ON i.id=e.instance_id AND e.user_id=i.user_id", "e.last_activity_at", "1=1"},
		{"team", "team_tasks e JOIN teams rt ON rt.id=e.team_id JOIN team_members rm ON rm.id=e.target_member_id AND rm.team_id=rt.id JOIN instances i ON i.id=rm.instance_id AND rt.user_id=i.user_id", "e.updated_at", "rt.resource_scope='cm' AND rt.status<>'deleted'"},
		{"runtime", "gateway_runtime_tasks e JOIN instances i ON i.id=e.instance_id AND e.owner_user_id=i.user_id AND e.generation=i.runtime_generation", "e.updated_at", "e.revoked=FALSE"},
	} {
		rows, err := s.sess.SQL().QueryContext(ctx, "SELECT i.id,MAX("+source.at+") FROM "+source.from+" WHERE "+instanceScope+" AND "+source.where+" AND "+source.at+">=? AND "+source.at+"<? GROUP BY i.id ORDER BY 2 DESC,i.id DESC LIMIT 7", id, now.AddDate(0, 0, -90), now)
		if err != nil {
			continue
		}
		for rows.Next() {
			var u use
			u.kind = source.kind
			if err = rows.Scan(&u.id, &u.at); err != nil {
				break
			}
			u.at = u.at.UTC()
			if prior, ok := latest[u.id]; !ok || u.at.After(prior.at) {
				latest[u.id] = u
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil {
			complete++
		}
	}
	d.Coverage.Recent = ready()
	if complete < 4 {
		d.Coverage.Recent = Coverage{Status: "partial", Note: "部分近期活动来源暂不可用，仅显示已确认记录"}
	}
	if complete == 0 {
		d.Coverage.Recent = unavailable("近期活动暂不可用")
	}
	ordered := make([]use, 0, len(latest))
	for _, u := range latest {
		ordered = append(ordered, u)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].at.Equal(ordered[j].at) {
			return ordered[i].id > ordered[j].id
		}
		return ordered[i].at.After(ordered[j].at)
	})
	d.RecentTruncated = len(ordered) > 6
	if len(ordered) > 6 {
		ordered = ordered[:6]
	}
	if len(ordered) == 0 {
		return
	}
	// Fetch authorized metadata separately from the 256-row presentation list.
	ids := make([]any, 0, len(ordered))
	for _, u := range ordered {
		ids = append(ids, u.id)
	}
	read := func(evidence bool) ([]RecentAgent, error) {
		expression, joins := stateExpr, stateJoins
		args := []any{now.Add(-10 * time.Minute), now, now.Add(-10 * time.Minute), now, now.Add(-10 * time.Minute), now, id}
		if !evidence {
			expression = `CASE WHEN i.status IN ('error','failed') THEN 'failed' WHEN i.status IN ('creating','starting','restarting','pending') THEN 'starting' WHEN i.status IN ('stopped','deleted','deleting') THEN 'offline' WHEN i.status='running' THEN 'running' ELSE 'unknown' END`
			joins = strings.Replace(stateJoins, ` LEFT JOIN instance_model_activity a ON a.instance_id=i.id AND a.generation=i.runtime_generation`, "", 1)
			args = []any{id}
		}
		args = append(args, ids...)
		rows, err := s.sess.SQL().QueryContext(ctx, `SELECT i.id,i.name,i.type,i.instance_mode,i.status,`+expression+`,t.id,COALESCE(t.name,''),i.cpu_cores,i.memory_gb,i.disk_gb,i.created_at,i.updated_at FROM instances i`+joins+` WHERE `+instanceScope+` AND i.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []RecentAgent{}
		for rows.Next() {
			var a RecentAgent
			if err = rows.Scan(&a.ID, &a.Name, &a.Type, &a.InstanceMode, &a.Status, &a.State, &a.TeamID, &a.TeamName, &a.CPUCores, &a.MemoryGB, &a.StorageGB, &a.CreatedAt, &a.UpdatedAt); err != nil {
				return nil, err
			}
			a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
			a.LastUsedAt, a.ActivityKind = latest[a.ID].at, latest[a.ID].kind
			result = append(result, a)
		}
		return result, rows.Err()
	}
	agents, err := read(true)
	if err != nil {
		agents, err = read(false)
		d.Coverage.Recent = Coverage{Status: "partial", Note: "近期活动或状态读取不完整"}
	}
	if err != nil {
		d.Coverage.Recent = unavailable("近期 Agent 元数据暂不可用")
		return
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].LastUsedAt.Equal(agents[j].LastUsedAt) {
			return agents[i].ID > agents[j].ID
		}
		return agents[i].LastUsedAt.After(agents[j].LastUsedAt)
	})
	d.RecentAgents = agents
}
