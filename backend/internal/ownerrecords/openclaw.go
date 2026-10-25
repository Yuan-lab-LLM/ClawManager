package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type OpenClawEvent struct{ Kind, RunID, Session string }

// DecodeOpenClaw inspects a bounded copy; it never rewrites protocol frames.
// Recognized contract: chat.send idempotencyKey and matching chat/final runId.
func DecodeOpenClaw(frame []byte, upstream bool) *OpenClawEvent {
	if len(frame) == 0 || len(frame) > 256<<10 {
		return nil
	}
	var p struct {
		Type   string `json:"type"`
		Method string `json:"method"`
		Event  string `json:"event"`
		Params struct {
			Key     string `json:"idempotencyKey"`
			Session string `json:"sessionKey"`
			Message string `json:"message"`
		} `json:"params"`
		Payload struct {
			Run     string `json:"runId"`
			Session string `json:"sessionKey"`
			State   string `json:"state"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"payload"`
	}
	if json.Unmarshal(frame, &p) != nil {
		return nil
	}
	valid := func(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 255 }
	if !upstream && p.Type == "req" && p.Method == "chat.send" && valid(p.Params.Key) && valid(p.Params.Session) && strings.TrimSpace(p.Params.Message) != "" {
		return &OpenClawEvent{Kind: "submitted", RunID: p.Params.Key, Session: p.Params.Session}
	}
	if upstream && p.Type == "event" && p.Event == "chat" && p.Payload.State == "final" && valid(p.Payload.Run) && valid(p.Payload.Session) && p.Payload.Message.Role == "assistant" && hasText(p.Payload.Message.Content) {
		return &OpenClawEvent{Kind: "completed", RunID: p.Payload.Run, Session: p.Payload.Session}
	}
	return nil
}

func hasText(raw json.RawMessage) bool {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text) != ""
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return false
	}
	for _, p := range parts {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			return true
		}
	}
	return false
}

// ObserveOpenClaw persists submissions before crediting final responses. Finals
// with no observed human submission are not credited (e.g. scheduled agent work).
func (s *Store) ObserveOpenClaw(ctx context.Context, instance uint64, event OpenClawEvent, at time.Time) error {
	identity, err := s.Bind(ctx, instance)
	if err != nil {
		return err
	}
	if identity.Runtime != "openclaw" {
		return fmt.Errorf("protocol/runtime mismatch")
	}
	key := Key("question_pending", strconv.FormatUint(instance, 10), event.Session, event.RunID)
	if event.Kind == "submitted" {
		_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records
   (record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,instance_mode,session_key,question_key,status,occurred_at)
   VALUES ('question_pending',?,?,?,?,?,?,?,?,?,'observed',?) ON DUPLICATE KEY UPDATE record_key=record_key`, key, identity.Scope, identity.Owner, identity.Owner, instance, identity.Runtime, identity.Mode, event.Session, event.RunID, at.UTC())
		return err
	}
	if event.Kind != "completed" {
		return fmt.Errorf("invalid question event")
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT occurred_at FROM northbound_owner_records WHERE record_type='question_pending' AND record_key=?`, key)
	if err != nil {
		return err
	}
	var submitted time.Time
	if err = row.Scan(&submitted); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	_, err = s.Apply(ctx, *identity, Fact{Type: "question", Source: "openclaw_chat", SourceID: event.RunID, SessionID: event.Session, QuestionID: event.RunID, At: at, SubmittedAt: submitted})
	return err
}
