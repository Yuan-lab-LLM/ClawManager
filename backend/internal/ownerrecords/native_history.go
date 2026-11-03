package ownerrecords

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Statistics readers return identifiers only. The separate recent-question
// projection populates Text transiently; it never enters statistical facts.
type NativeQuestion struct {
	Session   string  `json:"session"`
	ID        string  `json:"id"`
	At        float64 `json:"at"`
	Submitted float64 `json:"submitted"`
	Text      string  `json:"text,omitempty"`
}

const openCodeQuestionSQL = `PRAGMA query_only=ON;
SELECT u.session_id AS session,u.id AS id,MIN(json_extract(a.data,'$.time.completed'))/1000.0 AS at,COALESCE(json_extract(u.data,'$.time.created'),0)/1000.0 AS submitted
FROM message u JOIN session s ON s.id=u.session_id
JOIN message a ON a.session_id=u.session_id AND json_extract(a.data,'$.parentID')=u.id
WHERE s.parent_id IS NULL AND json_extract(u.data,'$.role')='user'
AND json_extract(a.data,'$.role')='assistant' AND json_extract(a.data,'$.finish') IN ('stop','end_turn')
AND json_extract(a.data,'$.error') IS NULL AND json_extract(a.data,'$.time.completed')>0
AND EXISTS(SELECT 1 FROM part p WHERE p.message_id=u.id AND json_extract(p.data,'$.type')='text' AND COALESCE(json_extract(p.data,'$.synthetic'),0)=0 AND LENGTH(TRIM(json_extract(p.data,'$.text')))>0)
AND EXISTS(SELECT 1 FROM part p WHERE p.message_id=a.id AND json_extract(p.data,'$.type')='text' AND LENGTH(TRIM(json_extract(p.data,'$.text')))>0)
GROUP BY u.session_id,u.id ORDER BY at,u.id LIMIT 10001;`

const hermesQuestionSQL = `PRAGMA query_only=ON;
SELECT u.session_id AS session,CAST(u.id AS TEXT) AS id,MIN(a.timestamp) AS at,u.timestamp AS submitted
FROM messages u JOIN sessions s ON s.id=u.session_id
JOIN messages a ON a.session_id=u.session_id AND a.id>u.id
WHERE s.parent_session_id IS NULL AND s.source IN ('desktop','web','tui','cli')
AND u.role='user' AND COALESCE(u.display_kind,'') IN ('','user','skill_invocation')
AND COALESCE(u._compressed_summary,0)=0 AND u.active=1 AND LENGTH(TRIM(u.content))>0
AND a.role='assistant' AND a.active=1 AND a.finish_reason IN ('stop','end_turn')
AND COALESCE(a.tool_calls,'') IN ('','[]','null') AND LENGTH(TRIM(a.content))>0
AND NOT EXISTS(SELECT 1 FROM messages n WHERE n.session_id=u.session_id AND n.role='user' AND n.id>u.id AND n.id<a.id)
GROUP BY u.session_id,u.id ORDER BY at,u.id LIMIT 10001;`

type cappedOutput struct {
	bytes.Buffer
	max int
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("history output limit exceeded")
	}
	return b.Buffer.Write(p)
}

// ReadNativeHistory is restricted to managed Lite directories. Unknown schemas,
// oversized histories and symlinks fail closed instead of fabricating a total.
func ReadNativeHistory(ctx context.Context, root, workspace string, identity Identity) ([]NativeQuestion, error) {
	return readNativeHistory(ctx, root, workspace, identity, false)
}

func readNativeHistory(ctx context.Context, root, workspace string, identity Identity, recent bool, cutoff ...time.Time) ([]NativeQuestion, error) {
	switch identity.Runtime {
	case "openclaw", "hermes", "opencode", "deepseek-harness":
	default:
		return nil, fmt.Errorf("unsupported native history")
	}
	expected := filepath.Join(root, identity.Runtime, fmt.Sprintf("user-%d", identity.Scope), fmt.Sprintf("instance-%d", identity.Instance))
	if !filepath.IsAbs(root) || filepath.Clean(workspace) != expected {
		return nil, fmt.Errorf("workspace identity mismatch")
	}
	var result []NativeQuestion
	if identity.Runtime == "hermes" || identity.Runtime == "opencode" {
		relative := "home/.hermes/state.db"
		query := hermesQuestionSQL
		if identity.Runtime == "opencode" {
			relative = "home/.local/share/opencode/opencode.db"
			query = openCodeQuestionSQL
		}
		if recent {
			query = hermesRecentSQL
			if identity.Runtime == "opencode" {
				query = openCodeRecentSQL
			}
		}
		if len(cutoff) > 0 && !recent {
			condition := fmt.Sprintf("u.timestamp>=%.6f AND ", float64(cutoff[0].UnixMilli())/1000)
			if identity.Runtime == "opencode" {
				condition = fmt.Sprintf("u.time_created>=%d AND ", cutoff[0].UnixMilli())
			}
			query = strings.Replace(query, "WHERE ", "WHERE "+condition, 1)
		}
		path := filepath.Join(expected, filepath.FromSlash(relative))
		if err := checkHistoryPath(root, path); err != nil {
			return nil, err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Lstat(path + suffix); err == nil {
				if err = checkHistoryPath(root, path+suffix); err != nil {
					return nil, err
				}
			}
		}
		cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", path, query)
		output := &cappedOutput{max: 4 << 20}
		cmd.Stdout = output
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("native history read failed: %w", err)
		}
		if output.Len() == 0 {
			return []NativeQuestion{}, nil
		}
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			return nil, err
		}
		if len(result) > 10000 {
			return nil, fmt.Errorf("native history page limit exceeded")
		}
		return result, nil
	}
	relative := "home/.openclaw/agents"
	if identity.Runtime == "deepseek-harness" {
		relative = "home/.dsh/sessions"
	}
	base := filepath.Join(expected, filepath.FromSlash(relative))
	if err := checkHistoryPath(root, base); err != nil {
		return nil, err
	}
	count := 0
	files := 0
	truncatedWithoutQuestion := false
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 2000 {
			return fmt.Errorf("history directory limit exceeded")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("history symlink rejected")
		}
		if d.IsDir() {
			return nil
		}
		eligible := identity.Runtime == "openclaw" && filepath.Base(filepath.Dir(path)) == "sessions" && strings.HasSuffix(path, ".jsonl") && !strings.Contains(d.Name(), ".trajectory.")
		if identity.Runtime == "deepseek-harness" {
			eligible = d.Name() == "session.jsonl.zstd"
		}
		if !eligible {
			return nil
		}
		if len(cutoff) > 0 {
			info, e := d.Info()
			if e != nil {
				return e
			}
			if info.ModTime().Before(cutoff[0]) {
				return nil
			}
		}
		files++
		if files > 200 {
			return fmt.Errorf("history file limit exceeded")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		var reader io.Reader = f
		truncated := false
		if (len(cutoff) > 0 || recent) && identity.Runtime == "openclaw" {
			// Bounded suffix inspection, not a full legacy-history import. Missing
			// ancestry is intentionally uncounted rather than guessed.
			info, e := f.Stat()
			if e != nil {
				return e
			}
			if info.Size() > 256<<10 {
				truncated = true
				header, e := bufio.NewReader(io.LimitReader(f, 65536)).ReadBytes('\n')
				if e != nil {
					return nil
				}
				if _, e = f.Seek(-(256 << 10), io.SeekEnd); e != nil {
					return e
				}
				tail := bufio.NewReader(f)
				if _, e = tail.ReadBytes('\n'); e != nil {
					return nil
				}
				reader = io.MultiReader(bytes.NewReader(header), tail)
			}
		}
		if identity.Runtime == "deepseek-harness" {
			z, err := zstd.NewReader(f, zstd.WithDecoderMaxMemory(32<<20), zstd.WithDecoderConcurrency(1))
			if err != nil {
				return err
			}
			defer z.Close()
			reader = z
		}
		limit := int64(16 << 20)
		if len(cutoff) > 0 || recent {
			limit = 1 << 20
		}
		data, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return err
		}
		if int64(len(data)) > limit {
			return fmt.Errorf("history file limit exceeded")
		}
		parse := ParseNativeLog
		if recent {
			parse = parseRecentNativeLog
		}
		questions, err := parse(identity.Runtime, data)
		if err != nil {
			return err
		}
		if recent && truncated && len(questions) == 0 {
			truncatedWithoutQuestion = true
		}
		for _, q := range questions {
			if len(cutoff) > 0 && !recent && (q.Submitted <= 0 || q.Submitted < float64(cutoff[0].UnixMilli())/1000) {
				continue
			}
			result = append(result, q)
		}
		if len(result) > 10000 {
			return fmt.Errorf("history evidence limit exceeded")
		}
		return nil
	})
	if err == nil && truncatedWithoutQuestion {
		return nil, fmt.Errorf("recent question outside bounded history window")
	}
	return result, err
}

func checkHistoryPath(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("history escapes root")
	}
	current := root
	for _, part := range append([]string{""}, strings.Split(relative, string(os.PathSeparator))...) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("history symlink rejected")
		}
	}
	return nil
}

func ParseNativeLog(runtime string, data []byte) ([]NativeQuestion, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	session := ""
	var pending []NativeQuestion
	goodReply := false
	turn := ""
	ancestors := map[string]NativeQuestion{}
	var result []NativeQuestion
	for scanner.Scan() {
		var packet struct {
			Type            string          `json:"type"`
			ID              string          `json:"id"`
			Parent          string          `json:"parentId"`
			Timestamp       json.RawMessage `json:"timestamp"`
			Time            float64         `json:"time"`
			DelegationDepth int             `json:"delegationDepth"`
			Message         struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Stop    string          `json:"stopReason"`
				Key     string          `json:"idempotencyKey"`
				Origin  struct {
					Owner bool `json:"senderIsOwner"`
				} `json:"__openclaw"`
			} `json:"message"`
			Data struct {
				Turn    json.RawMessage `json:"turn"`
				ID      string          `json:"id"`
				Content json.RawMessage `json:"content"`
				Source  struct {
					Kind string `json:"kind"`
				} `json:"source"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
				Reason json.RawMessage `json:"reason"`
			} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &packet); err != nil {
			return nil, fmt.Errorf("incomplete history record: %w", err)
		}
		if packet.Type == "session" {
			session = packet.ID
			if packet.DelegationDepth > 0 {
				return []NativeQuestion{}, nil
			}
			continue
		}
		if session == "" {
			continue
		}
		if runtime == "openclaw" && packet.ID != "" {
			ancestors[packet.ID] = ancestors[packet.Parent]
		}
		if runtime == "openclaw" && packet.Type == "message" {
			m := packet.Message
			if m.Role == "user" {
				ancestors[packet.ID] = NativeQuestion{}
				if m.Origin.Owner && hasText(m.Content) && packet.ID != "" {
					id := m.Key
					if id == "" {
						id = "row:" + session + ":" + packet.ID
					}
					var timestamp string
					_ = json.Unmarshal(packet.Timestamp, &timestamp)
					submitted, _ := time.Parse(time.RFC3339Nano, timestamp)
					at := float64(0)
					if !submitted.IsZero() {
						at = float64(submitted.UnixMilli()) / 1000
					}
					ancestors[packet.ID] = NativeQuestion{Session: session, ID: id, Submitted: at}
				}
				continue
			}
			question := ancestors[packet.ID]
			if m.Role == "assistant" && (m.Stop == "stop" || m.Stop == "end_turn") && hasText(m.Content) && question.ID != "" {
				var timestamp string
				if err := json.Unmarshal(packet.Timestamp, &timestamp); err != nil {
					return nil, fmt.Errorf("invalid OpenClaw timestamp")
				}
				at, err := time.Parse(time.RFC3339Nano, timestamp)
				if err != nil {
					return nil, err
				}
				question.At = float64(at.UnixMilli()) / 1000
				result = append(result, question)
			}
		}
		if runtime == "deepseek-harness" {
			switch packet.Type {
			case "turn/start":
				turn = string(packet.Data.Turn)
				pending = nil
				goodReply = false
			case "user/message":
				if turn != "" && packet.Data.Source.Kind == "user" && packet.Data.ID != "" && hasText(packet.Data.Content) {
					pending = append(pending, NativeQuestion{Session: session, ID: packet.Data.ID, Submitted: packet.Time / 1000})
				}
			case "assistant/message":
				if turn != "" && string(packet.Data.Turn) == turn && hasText(packet.Data.Message.Content) {
					goodReply = true
				}
			case "turn/end":
				if string(packet.Data.Turn) != turn {
					continue
				}
				var reason struct {
					Kind string `json:"kind"`
				}
				_ = json.Unmarshal(packet.Data.Reason, &reason)
				if reason.Kind == "completed" && goodReply {
					for _, q := range pending {
						q.At = packet.Time / 1000
						result = append(result, q)
					}
				}
				pending = nil
				goodReply = false
				turn = ""
			}
		}
	}
	return result, scanner.Err()
}
