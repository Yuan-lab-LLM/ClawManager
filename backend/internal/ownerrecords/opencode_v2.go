package ownerrecords

import (
	"context"
	"encoding/json"
)

// OpenCode 2.0.27 materializes turns in session_message, independently of the
// optional event log. Team sessions are automation, not additional human asks.
const openCodeV2QuestionSQL = `PRAGMA query_only=ON;
SELECT u.session_id AS session,u.id AS id,MIN(json_extract(a.data,'$.time.completed'))/1000.0 AS at,
u.time_created/1000.0 AS submitted
FROM session_message u JOIN session_v2 s ON s.id=u.session_id
JOIN session_message a ON a.session_id=u.session_id AND a.seq>u.seq AND a.type='assistant'
WHERE s.parent_id IS NULL AND json_extract(COALESCE(s.metadata,'{}'),'$."clawmanager.team"') IS NULL
AND u.type='user' AND LENGTH(TRIM(json_extract(u.data,'$.text')))>0
AND json_extract(a.data,'$.finish') IN ('stop','end_turn')
AND json_extract(a.data,'$.error') IS NULL AND json_extract(a.data,'$.time.completed')>0
AND EXISTS(SELECT 1 FROM json_each(a.data,'$.content') p WHERE json_extract(p.value,'$.type')='text' AND LENGTH(TRIM(json_extract(p.value,'$.text')))>0)
AND NOT EXISTS(SELECT 1 FROM session_message n WHERE n.session_id=u.session_id AND n.seq>u.seq AND n.seq<a.seq AND n.type IN ('user','synthetic'))
GROUP BY u.session_id,u.id ORDER BY at,u.id LIMIT 10001;`

const openCodeV2RecentSQL = `PRAGMA query_only=ON;
SELECT u.session_id AS session,u.id AS id,u.time_created/1000.0 AS at,SUBSTR(json_extract(u.data,'$.text'),1,4096) AS text
FROM session_message u JOIN session_v2 s ON s.id=u.session_id
WHERE s.parent_id IS NULL AND json_extract(COALESCE(s.metadata,'{}'),'$."clawmanager.team"') IS NULL
AND u.type='user' AND LENGTH(TRIM(json_extract(u.data,'$.text')))>0
ORDER BY u.time_created DESC,u.id DESC LIMIT 1;`

func openCodeHistoryQuery(ctx context.Context, path string, recent bool) (string, error) {
	raw, err := readHistorySQLite(ctx, path, `SELECT name FROM sqlite_master WHERE type='table' AND name IN ('session_v2','session_message');`, 4096)
	if err != nil {
		return "", err
	}
	var tables []struct {
		Name string `json:"name"`
	}
	if err = json.Unmarshal(raw, &tables); err != nil {
		return "", errHistoryRestricted
	}
	if len(tables) == 2 {
		if recent {
			return openCodeV2RecentSQL, nil
		}
		return openCodeV2QuestionSQL, nil
	}
	if len(tables) != 0 {
		return "", errHistoryRestricted
	}
	if recent {
		return openCodeRecentSQL, nil
	}
	return openCodeQuestionSQL, nil
}
