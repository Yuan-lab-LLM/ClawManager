package ownerrecords

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const hermesRecentSQL = `PRAGMA query_only=ON;
SELECT u.session_id AS session,CAST(u.id AS TEXT) AS id,u.timestamp AS at,SUBSTR(u.content,1,4096) AS text
FROM messages u JOIN sessions s ON s.id=u.session_id
WHERE s.parent_session_id IS NULL AND s.source IN ('desktop','web','tui','cli')
AND u.role='user' AND COALESCE(u.display_kind,'') IN ('','user','skill_invocation')
AND COALESCE(u._compressed_summary,0)=0 AND u.active=1 AND LENGTH(TRIM(u.content))>0
ORDER BY u.timestamp DESC,u.id DESC LIMIT 1;`

const openCodeRecentSQL = `PRAGMA query_only=ON;
SELECT u.session_id AS session,u.id AS id,json_extract(u.data,'$.time.created')/1000.0 AS at,
(SELECT SUBSTR(json_extract(p.data,'$.text'),1,4096) FROM part p WHERE p.message_id=u.id
AND json_extract(p.data,'$.type')='text' AND COALESCE(json_extract(p.data,'$.synthetic'),0)=0
AND LENGTH(TRIM(json_extract(p.data,'$.text')))>0 ORDER BY p.id LIMIT 1) AS text
FROM message u JOIN session s ON s.id=u.session_id WHERE s.parent_id IS NULL AND json_extract(u.data,'$.role')='user'
AND EXISTS(SELECT 1 FROM part p WHERE p.message_id=u.id AND json_extract(p.data,'$.type')='text' AND COALESCE(json_extract(p.data,'$.synthetic'),0)=0 AND LENGTH(TRIM(json_extract(p.data,'$.text')))>0)
ORDER BY json_extract(u.data,'$.time.created') DESC,u.id DESC LIMIT 1;`

var recentTags = regexp.MustCompile(`<[^>]*>`)
var recentSecret = regexp.MustCompile(`(?i)(bearer\s+\S+|(?:sk|sk-proj)-[a-z0-9_-]{12,}|(?:password|passwd|api[_-]?key|secret|token|密码|密钥)\s*[:=：]\s*\S+)`)

func questionExcerpt(raw string) string {
	if strings.Contains(raw, "-----BEGIN ") {
		return "[含敏感信息，摘要已隐藏]"
	}
	raw = recentSecret.ReplaceAllString(raw, "[敏感信息已隐藏]")
	raw = recentTags.ReplaceAllString(raw, "")
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, raw)
	raw = strings.Join(strings.Fields(raw), " ")
	r := []rune(raw)
	if len(r) > 120 {
		raw = string(r[:120]) + "…"
	}
	return raw
}

func humanText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Synthetic bool   `json:"synthetic"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if (p.Type == "text" || p.Type == "input_text") && !p.Synthetic {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, " ")
}

// A latest human prompt is independent of whether its answer completed. Never
// substitute a session title, assistant reply or model-request history.
func parseRecentNativeLog(runtime string, data []byte) ([]NativeQuestion, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	session := ""
	out := []NativeQuestion{}
	for scanner.Scan() {
		var p struct {
			Type, ID        string
			Timestamp       string
			Time            float64
			DelegationDepth int
			Message         struct {
				Role    string
				Content json.RawMessage
				Key     string `json:"idempotencyKey"`
				Origin  struct {
					Owner bool `json:"senderIsOwner"`
				} `json:"__openclaw"`
			}
			Data struct {
				ID      string
				Content json.RawMessage
				Source  struct{ Kind string }
			}
		}
		if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
			return nil, err
		}
		if p.Type == "session" {
			session = p.ID
			if p.DelegationDepth > 0 {
				return out, nil
			}
			continue
		}
		if session == "" {
			continue
		}
		q := NativeQuestion{Session: session}
		if runtime == "openclaw" && p.Type == "message" && p.Message.Role == "user" && p.Message.Origin.Owner && p.ID != "" {
			q.ID = p.Message.Key
			if q.ID == "" {
				q.ID = "row:" + session + ":" + p.ID
			}
			q.Text = humanText(p.Message.Content)
			at, err := time.Parse(time.RFC3339Nano, p.Timestamp)
			if err != nil {
				continue
			}
			q.At = float64(at.UnixMilli()) / 1000
		} else if runtime == "deepseek-harness" && p.Type == "user/message" && p.Data.Source.Kind == "user" {
			q.ID = p.Data.ID
			q.Text = humanText(p.Data.Content)
			q.At = p.Time / 1000
		}
		if q.ID != "" && strings.TrimSpace(q.Text) != "" {
			q.Text = questionExcerpt(q.Text)
			out = append(out, q)
		}
	}
	return out, scanner.Err()
}

type RecentQuestion struct {
	Text      string `json:"text"`
	At        string `json:"at"`
	CheckedAt string `json:"checked_at"`
}

// Cache replaces a complete read-only snapshot, not a stream of partial facts.
// Snapshot ordering rejects late scans; replacement also handles message deletion.
func (s *Store) cacheRecentQuestion(ctx context.Context, i Identity, workspace string) error {
	checked := time.Now().UTC()
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	qs, readErr := readNativeHistory(readCtx, s.HistoryRoot, workspace, i, true)
	cancel()
	// A failed read is not evidence that the last question was deleted.
	if readErr != nil {
		return readErr
	}
	var latest NativeQuestion
	if readErr == nil {
		for _, q := range qs {
			if q.At > 0 && q.At <= float64(checked.Add(5*time.Minute).Unix()) && (q.At > latest.At || q.At == latest.At && q.ID > latest.ID) {
				latest = q
			}
		}
	}
	payload := "{}"
	status := "unavailable"
	if readErr == nil {
		status = "empty"
	}
	if latest.ID != "" {
		status = "ready"
		b, _ := json.Marshal(RecentQuestion{Text: questionExcerpt(latest.Text), At: time.UnixMilli(int64(latest.At * 1000)).UTC().Format(time.RFC3339), CheckedAt: checked.Format(time.RFC3339)})
		payload = string(b)
	}
	key := Key("recent_question", strconv.FormatUint(i.Instance, 10))
	row, lookupErr := s.Session.SQL().QueryRowContext(ctx, `SELECT r.payload,r.status FROM northbound_owner_records r JOIN instances i ON i.id=r.instance_id AND r.source_key=SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256) WHERE r.record_type='recent_question' AND r.record_key=? AND r.scope_id=? AND r.owner_normalized=?`, key, i.Scope, strings.ToLower(strings.TrimSpace(i.Owner)))
	if lookupErr != nil {
		return lookupErr
	}
	var previous []byte
	var previousStatus string
	if e := row.Scan(&previous, &previousStatus); e == nil {
		var old, next RecentQuestion
		if json.Unmarshal(previous, &old) == nil && json.Unmarshal([]byte(payload), &next) == nil && previousStatus == status && old.Text == next.Text && old.At == next.At {
			return nil
		}
	} else if e != sql.ErrNoRows {
		return e
	}
	// Avoid the ORM's failed-query argument logger for private excerpts.
	driver, ok := s.Session.Driver().(*sql.DB)
	if !ok {
		return fmt.Errorf("recent question SQL driver unavailable")
	}
	_, err := driver.ExecContext(ctx, `INSERT INTO northbound_owner_records
 (record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,status,occurred_at,expires_at,payload,source_key)
 SELECT 'recent_question',?,i.user_id,i.owner,LOWER(TRIM(i.owner)),i.id,i.type,?,?,?,CAST(? AS JSON),SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256) FROM instances i
 WHERE i.id=? AND i.user_id=? AND LOWER(TRIM(i.owner))=? AND i.workspace_path=?
 ON DUPLICATE KEY UPDATE source_key=IF(VALUES(occurred_at)>=northbound_owner_records.occurred_at,VALUES(source_key),northbound_owner_records.source_key),payload=IF(VALUES(occurred_at)>=northbound_owner_records.occurred_at,VALUES(payload),northbound_owner_records.payload),status=IF(VALUES(occurred_at)>=northbound_owner_records.occurred_at,VALUES(status),northbound_owner_records.status),
 scope_id=IF(VALUES(occurred_at)>=northbound_owner_records.occurred_at,VALUES(scope_id),northbound_owner_records.scope_id),owner_normalized=IF(VALUES(occurred_at)>=northbound_owner_records.occurred_at,VALUES(owner_normalized),northbound_owner_records.owner_normalized),
 owner=IF(VALUES(occurred_at)>=northbound_owner_records.occurred_at,VALUES(owner),northbound_owner_records.owner),expires_at=GREATEST(northbound_owner_records.expires_at,VALUES(expires_at)),occurred_at=GREATEST(northbound_owner_records.occurred_at,VALUES(occurred_at))`, key, status, checked, checked.Add(30*time.Minute), payload, i.Instance, i.Scope, strings.ToLower(strings.TrimSpace(i.Owner)), workspace)
	return err
}

func (s *Store) recentQuestions(ctx context.Context, owner string, scope *uint64, now time.Time) (map[uint64]RecentQuestion, error) {
	result := map[uint64]RecentQuestion{}
	query := `SELECT r.instance_id,r.payload FROM northbound_owner_records r JOIN instances i ON i.id=r.instance_id AND i.user_id=r.scope_id AND LOWER(TRIM(i.owner))=r.owner_normalized AND r.source_key=SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256) WHERE r.record_type='recent_question' AND r.owner_normalized=? AND r.status='ready'`
	args := []any{owner}
	if scope != nil {
		query += " AND r.scope_id=?"
		args = append(args, *scope)
	}
	rows, err := s.Session.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var b []byte
		if err = rows.Scan(&id, &b); err != nil {
			return result, err
		}
		var q RecentQuestion
		if json.Unmarshal(b, &q) == nil && q.Text != "" {
			result[id] = q
		}
	}
	return result, rows.Err()
}
