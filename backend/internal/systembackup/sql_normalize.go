package systembackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"clawreef/internal/systembackup/team"
)

// NormalizeDataRow operates on the isolated data copy only. Every original
// field remains in the immutable encrypted source archive and Change.Before.
// It does not emit Team completion events or schedule any business operation.
func NormalizeDataRow(table SQLTable, row team.Row, now time.Time) (team.Change, error) {
	policy, ok := Policy(table.Name)
	if !ok || policy.Category == "system_backup" {
		return team.Change{}, errors.New("normalization policy missing")
	}
	if policy.Category == "teams" || table.Name == "northbound_operations" {
		change, e := team.Normalize(table.Name, row, now)
		if e != nil {
			return change, e
		}
		// Use the same declared precision for direct import and stream
		// normalization, so original-state evidence is stable across retries.
		for _, column := range table.Columns {
			if column.Name == "finished_at" && row[column.Name].IsNull && !change.After[column.Name].IsNull && change.Reason == "restore_interrupted" {
				value, e := sqlCell(now, column)
				if e != nil {
					return change, e
				}
				change.After[column.Name] = value
			}
		}
		return change, nil
	}
	change := team.Change{Table: table.Name, Before: team.Clone(row), After: team.Clone(row), Reason: "preserved"}
	set := func(name, value string) {
		if _, ok := row[name]; ok {
			change.After[name] = team.Value(value)
			change.Reason = "restore_disabled"
			change.Quarantined = true
		}
	}
	erase := func(names ...string) {
		for _, name := range names {
			if _, ok := row[name]; !ok {
				continue
			}
			for _, c := range table.Columns {
				if c.Name == name {
					if c.Nullable == "YES" {
						change.After[name] = team.Null()
					} else {
						change.After[name] = team.Value("")
					}
					change.Reason = "restore_disabled"
					change.Quarantined = true
					break
				}
			}
		}
	}
	stopState := func(field, target string, active, terminal []string) error {
		old, ok := row[field]
		if !ok || old.IsNull {
			return errors.New("required runtime state missing")
		}
		status := string(old.Data)
		if !slices.Contains(active, status) && !slices.Contains(terminal, status) {
			return errors.New("unregistered runtime state")
		}
		change.Quarantined = true
		if slices.Contains(active, status) {
			set(field, target)
			change.Reason = "restore_interrupted"
		}
		return nil
	}
	var err error
	switch table.Name {
	case "users":
		set("is_active", "0")
		set("password_hash", "!disabled-after-isolated-restore!")
	case "instances":
		err = stopState("status", "stopped", []string{"creating", "running", "deleting"}, []string{"stopped", "error"})
		erase("access_token", "agent_bootstrap_token", "pod_name", "pod_namespace", "pod_ip", "access_url")
		set("environment_overrides_json", "{}")
	case "instance_agents":
		set("status", "offline")
		erase("session_token", "session_expires_at")
	case "instance_browser_workers":
		set("enabled", "0")
		set("status", "disabled")
	case "instance_desired_state":
		set("desired_power_state", "stopped")
		erase("desired_runtime_action")
	case "instance_runtime_status":
		set("infra_status", "stopped")
		set("agent_status", "offline")
		set("openclaw_status", "stopped")
	case "instance_runtime_bindings":
		set("state", "failed")
		erase("gateway_pid")
	case "runtime_pods":
		set("state", "offline")
		set("draining", "1")
		erase("pod_uid", "pod_ip", "agent_endpoint")
	case "instance_commands":
		err = stopState("status", "failed", []string{"pending", "dispatched", "running", "accepted", "batch_pending"}, []string{"succeeded", "failed", "cancelled", "canceled", "timeout", "timed_out", "expired"})
	case "runtime_rollouts":
		err = stopState("status", "error", []string{"pending", "running", "preflight_passed", "preflight_blocked"}, []string{"finished", "error", "cancelled", "canceled"})
		set("auto_rollback", "0")
		erase("operation_token", "operation_deadline_at")
	case "runtime_upgrade_items":
		err = stopState("state", "failed", []string{"pending", "prepared", "snapshotted", "restart_ready", "gateway_verified", "restored", "compatibility_checked", "quiescing", "quiesced", "migration_started", "migrated", "restarting", "restarted", "verifying", "activating", "rolling_back", "rollback_started", "isolated", "candidate_preparing", "candidate_ready"}, []string{"completed", "verified", "failed", "rolled_back", "skipped", "cancelled", "canceled", "rollback_failed"})
		erase("operation_token", "operation_phase", "operation_deadline_at")
	case "instance_lifecycle_batches":
		err = stopState("status", "cancelled", []string{"running", "paused", "cancelling"}, []string{"completed", "completed_with_errors", "cancelled"})
	case "instance_lifecycle_batch_items":
		err = stopState("state", "cancelled", []string{"pending", "active"}, []string{"succeeded", "failed", "warning", "cancelled"})
	case "backup_schedules":
		set("is_active", "0")
	case "backups":
		err = stopState("status", "failed", []string{"creating"}, []string{"completed", "failed", "deleted"})
	case "northbound_sessions":
		set("status", "revoked")
		set("access_expires_at", "2000-01-01 00:00:00")
		set("refresh_expires_at", "2000-01-01 00:00:00")
	case "northbound_auth_challenges":
		set("expires_at", "2000-01-01 00:00:00")
		set("status", "used")
	case "instance_gateway_token_aliases":
		set("expires_at", "2000-01-01 00:00:00")
	case "instance_external_access":
		set("enabled", "0")
		erase("public_token_hash", "short_code_hash", "api_key_hash", "password_value")
	case "llm_models":
		set("is_active", "0")
		erase("api_key", "api_key_secret_ref")
	case "gateway_keys":
		set("status", "revoked")
		set("expires_at", "2000-01-01 00:00:00")
	case "routing_classifier_settings":
		set("routing_mode", "rules")
		erase("base_url", "encrypted_api_key")
	case "gateway_image_artifacts":
		set("ready", "0")
		set("expires_at", "2000-01-01 00:00:00")
	case "gateway_image_jobs":
		err = stopState("state", "failed", []string{"preparing", "running"}, []string{"completed", "failed"})
		// Preserve charge/estimate and the original encrypted row for review;
		// an isolated restore must not settle or emit accounting a second time.
		set("settled", "1")
		set("analytics_recorded", "1")
	case "enterprise_auth_settings":
		set("enabled", "0")
		set("allow_local_fallback", "0")
		erase("ldap_bind_password_ciphertext")
	case "northbound_admin_settings":
		set("api_enabled", "0")
	case "northbound_caller_policies":
		set("enabled", "0")
		set("api_keys_json", "[]")
	case "skill_package_materialize_jobs":
		err = stopState("status", "failed", []string{"pending", "running", "retrying"}, []string{"succeeded", "failed", "cancelled"})
	case "security_scan_jobs", "security_scan_job_items":
		err = stopState("status", "failed", []string{"pending", "running"}, []string{"succeeded", "completed", "failed", "cancelled", "skipped"})
	default:
		// The remaining registered tables are immutable history or configuration.
		// The P0 drill never starts their consumers. Changing that boundary requires
		// a separate production-restore design and execution guards.
	}
	if err != nil {
		return change, err
	}
	// Fixed expiration values must use the same precision as MySQL readback.
	// Keep exact comparison; do not relax the verifier or rewrite saved history.
	for _, column := range table.Columns {
		kind := strings.ToLower(column.Type)
		if kind != "date" && !strings.HasPrefix(kind, "datetime") && !strings.HasPrefix(kind, "timestamp") {
			continue
		}
		before, exists := change.Before[column.Name]
		after := change.After[column.Name]
		if !exists || after.IsNull || (before.IsNull == after.IsNull && bytes.Equal(before.Data, after.Data)) {
			continue
		}
		parsed, e := time.Parse("2006-01-02 15:04:05.999999", string(after.Data))
		if e != nil {
			return change, errors.New("invalid normalized SQL timestamp")
		}
		value, e := sqlCell(parsed, column)
		if e != nil {
			return change, e
		}
		change.After[column.Name] = value
	}
	return change, nil
}

// NormalizeSQLSnapshot produces another verifiable stream; Before/After must
// be sent to an encrypted evidence sink. Unknown state/schema aborts the stream
// before its final seal, so a partial normalized copy can never be accepted.
func NormalizeSQLSnapshot(ctx context.Context, r io.Reader, w io.Writer, at time.Time, evidence func(SQLTable, team.Change) error) (SQLResult, error) {
	if at.IsZero() || evidence == nil {
		return SQLResult{}, errors.New("normalization evidence required")
	}
	encoder := json.NewEncoder(w)
	if e := encoder.Encode(SQLRecord{Kind: "mysql_rows_v1"}); e != nil {
		return SQLResult{}, e
	}
	out := SQLResult{Tables: map[string]int64{}, Hashes: map[string]string{}}
	var current *SQLTable
	var count int64
	hash := sha256.New()
	finish := func() error {
		if current == nil {
			return nil
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		out.Tables[current.Name] = count
		out.Hashes[current.Name] = digest
		return encoder.Encode(SQLRecord{Kind: "end_table", Count: count, SHA256: digest})
	}
	_, err := ReadSQLSnapshot(r, func(table SQLTable) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := finish(); e != nil {
			return e
		}
		current = &table
		count = 0
		hash.Reset()
		return encoder.Encode(SQLRecord{Kind: "table", Table: &table})
	}, func(table SQLTable, row team.Row) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		change, e := NormalizeDataRow(table, row, at)
		if e != nil {
			return e
		}
		// Match the timestamp precision declared by the target schema.
		for _, column := range table.Columns {
			if column.Name == "finished_at" && !change.After[column.Name].IsNull && row[column.Name].IsNull && change.Reason == "restore_interrupted" {
				v, e := sqlCell(at, column)
				if e != nil {
					return e
				}
				change.After[column.Name] = v
			}
		}
		if e = evidence(table, change); e != nil {
			return e
		}
		cells := make([]team.Cell, len(table.Columns))
		for i, c := range table.Columns {
			v, ok := change.After[c.Name]
			if !ok {
				return errors.New("normalization lost column")
			}
			cells[i] = v
		}
		raw, e := json.Marshal(SQLRecord{Kind: "row", Cells: cells})
		if e != nil {
			return e
		}
		raw = append(raw, '\n')
		hash.Write(raw)
		count++
		return writeFull(w, raw)
	})
	if err != nil {
		return out, err
	}
	if err = finish(); err != nil {
		return out, err
	}
	return out, encoder.Encode(SQLRecord{Kind: "end"})
}

func SnapshotBusinessTables(ctx context.Context, q SQLQueryer) ([]string, error) {
	return snapshotBusinessTables(ctx, q, false)
}
func snapshotBusinessTables(ctx context.Context, q SQLQueryer, teamData bool) ([]string, error) {
	names, e := discoverTables(ctx, q, teamData)
	if e != nil {
		return nil, e
	}
	business := []string{}
	for _, name := range names {
		p, _ := Policy(name)
		if p.Category != "system_backup" {
			business = append(business, name)
		}
	}
	rows, e := q.QueryContext(ctx, "SELECT TABLE_NAME,REFERENCED_TABLE_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND REFERENCED_TABLE_NAME IS NOT NULL ORDER BY TABLE_NAME,REFERENCED_TABLE_NAME")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	deps := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if e = rows.Scan(&child, &parent); e != nil {
			return nil, e
		}
		if strings.HasPrefix(child, "system_backup_") {
			continue
		}
		deps[child] = append(deps[child], parent)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return DependencyOrder(business, deps)
}
