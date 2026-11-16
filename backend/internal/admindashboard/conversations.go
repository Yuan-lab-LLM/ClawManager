package admindashboard

import (
	"context"
	"database/sql"
)

// Gateway persistence keeps only the current user turn. Tool rounds/retries
// reuse its trace; a root user message plus an attributed successful reply is
// one recorded conversation, independently of optional instance owners or XP.
const conversationTurnsSQL = `SELECT TIMESTAMPDIFF(HOUR,?,turn_at),COUNT(*) FROM (
 SELECT u.user_id,u.instance_id,u.session_id,u.trace_id,MIN(u.created_at) AS turn_at
 FROM chat_messages u
 WHERE u.role='user' AND u.sequence_no=1 AND u.trace_id<>'' AND u.session_id<>''
 AND u.user_id>0 AND u.created_at>=? AND u.created_at<?
 AND EXISTS (
  SELECT 1 FROM chat_messages a JOIN model_invocations v ON v.id=a.invocation_id
  WHERE a.trace_id=u.trace_id AND a.session_id=u.session_id AND a.user_id=u.user_id
  AND a.instance_id <=> u.instance_id AND a.role='assistant'
  AND a.created_at>=u.created_at AND a.created_at<?
  AND v.trace_id=u.trace_id AND v.session_id=u.session_id AND v.user_id=u.user_id
  AND v.instance_id <=> u.instance_id AND v.status='completed' AND v.traffic_class='llm'
 )
 GROUP BY u.user_id,u.instance_id,u.session_id,u.trace_id
) recorded_turns GROUP BY 1`

func (s *Store) conversations(ctx context.Context, d *Overview) {
	counts := make([]int64, len(d.History))
	err := s.read(ctx, conversationTurnsSQL, []any{d.Window.Start, d.Window.Start, d.Window.End, d.Window.End}, func(rows *sql.Rows) error {
		var i int
		var n int64
		if err := rows.Scan(&i, &n); err != nil {
			return err
		}
		if i >= 0 && i < len(counts) {
			counts[i] = n
		}
		return nil
	})
	if err != nil {
		return
	}
	var total int64
	for i, n := range counts {
		d.History[i].Conversations = ptr(n)
		total += n
	}
	d.Usage.Conversations = ptr(total)
	d.Coverage["conversations"] = Coverage{Status: "partial", Note: "按平台记录的用户/实例/会话/轮次标识去重，需首条用户消息及同归属成功 LLM 回复；含工具中间响应，不代表最终任务完成；相同轮次标识的调用和重试合并；按首条用户消息时间统计，不依赖负责人或成长采集起点；未经过平台网关的对话不计入"}
}
