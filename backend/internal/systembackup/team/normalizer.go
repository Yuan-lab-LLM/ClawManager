// Package team implements data-only Team restore policies. It deliberately
// does not call TeamService completion handlers or start message consumers.
package team

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Cell preserves SQL NULL, empty values, binary columns and integer precision.
// JSON encodes Data as base64; converting arbitrary SQL bytes to JSON strings
// would replace invalid UTF-8 and silently corrupt archived values.
type Cell struct {
	IsNull bool   `json:"null"`
	Data   []byte `json:"data,omitempty"`
}
type Row map[string]Cell

func Value(s string) Cell { return Cell{Data: []byte(s)} }
func Null() Cell          { return Cell{IsNull: true} }
func Clone(row Row) Row {
	out := make(Row, len(row))
	for k, v := range row {
		v.Data = bytesClone(v.Data)
		out[k] = v
	}
	return out
}
func bytesClone(v []byte) []byte {
	if v == nil {
		return nil
	}
	return append([]byte{}, v...)
}

type Change struct {
	Table       string `json:"table"`
	Before      Row    `json:"before"`
	After       Row    `json:"after"`
	Reason      string `json:"reason"`
	Quarantined bool   `json:"quarantined"`
}

// Normalize is pure. The restore coordinator must durably record Before once
// (run/table/primary-key uniqueness) before applying After, and preserve the
// original immutable artifact. Repeated normalization never replaces evidence.
func Normalize(table string, row Row, now time.Time) (Change, error) {
	r := Change{Table: table, Before: Clone(row), After: Clone(row), Reason: "preserved"}
	status := string(row["status"].Data)
	var active, terminal []string
	next := "failed"
	switch table {
	case "team_tasks":
		active = []string{"pending", "dispatched", "running", "waiting", "stale"}
		terminal = []string{"succeeded", "failed"}
		if field, ok := row["workflow_state"]; ok && (field.IsNull || !slices.Contains([]string{"open", "planning", "executing", "awaiting_phase_results", "awaiting_leader_decision", "synthesizing", "completion_pending", "completed", "failed"}, string(field.Data))) {
			return r, errors.New("unregistered task workflow state")
		}
	case "team_work_items":
		active = []string{"pending", "dispatched", "running", "waiting", "blocked", "awaiting_review", "planned"}
		terminal = []string{"succeeded", "failed", "cancelled", "skipped", "superseded"}
	case "team_workflow_phases":
		active = []string{"pending", "planned", "active", "awaiting_results", "awaiting_leader_decision"}
		terminal = []string{"done", "completed", "cancelled", "superseded"}
		next = "cancelled"
	case "team_event_outbox":
		active = []string{"pending"}
		terminal = []string{"delivered", "failed"}
		r.Quarantined = true
	case "northbound_operations":
		active = []string{"queued", "processing", "batch_pending"}
		terminal = []string{"succeeded", "failed", "canceled"}
		r.Quarantined = true
	case "owner_team_provision_steps":
		active = []string{"pending", "started"}
		terminal = []string{"done", "failed"}
		r.Quarantined = true
	case "team_members":
		active = []string{"creating", "idle", "busy", "deleting"}
		terminal = []string{"offline", "deleted", "failed"}
		next = "offline"
		r.Quarantined = true
	case "teams":
		active = []string{"creating", "running", "deleting"}
		terminal = []string{"failed", "deleted"}
		r.Quarantined = true
	case "team_events", "team_workspace_file_audits", "custom_team_templates", "owner_team_admission_locks":
		return r, nil
	default:
		return r, fmt.Errorf("normalization policy missing for %s", table)
	}
	if !slices.Contains(active, status) && !slices.Contains(terminal, status) {
		return r, fmt.Errorf("unregistered state for %s", table)
	}
	if slices.Contains(active, status) {
		r.After["status"] = Value(next)
		r.Reason = "restore_interrupted"
		r.Quarantined = true
		if table == "team_tasks" {
			if _, ok := row["workflow_state"]; ok {
				r.After["workflow_state"] = Value("failed")
			}
		}
		// Do not fabricate completion times for cancelled phases or erase an
		// existing error. The reason lives in durable restore evidence.
		if old, ok := row["finished_at"]; ok && old.IsNull {
			r.After["finished_at"] = Value(now.UTC().Format("2006-01-02 15:04:05.000000"))
		}
	}
	if table == "northbound_operations" {
		for _, key := range []string{"lease_owner", "lease_expires_at"} {
			if _, ok := row[key]; ok {
				r.After[key] = Null()
			}
		}
	}
	if table == "team_members" {
		for _, key := range []string{"current_task_id", "runtime_task_id"} {
			if _, ok := row[key]; ok {
				r.After[key] = Null()
			}
		}
		if _, ok := row["availability"]; ok {
			r.After["availability"] = Value("offline")
		}
	}
	if table == "teams" {
		for _, key := range []string{"redis_url_secret_name", "redis_url_secret_key", "team_token_secret_name", "team_token_secret_key"} {
			if _, ok := row[key]; ok {
				r.After[key] = Null()
			}
		}
	}
	return r, nil
}
