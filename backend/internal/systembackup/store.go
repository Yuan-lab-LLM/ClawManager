package systembackup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

type Store struct {
	DB             *sql.DB
	InstallationID string
}

var publicIDPattern = regexp.MustCompile(`^(sbk|sdr|spf|sav)_[a-f0-9]{32}$`)
var idemPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{8,128}$`)

func (s Store) Bootstrap(ctx context.Context) error {
	if !refPattern.MatchString(s.InstallationID) {
		return errors.New("invalid installation identity")
	}
	_, err := s.DB.ExecContext(ctx, "INSERT IGNORE INTO system_backup_installation_state(singleton_id,origin_installation_id) VALUES(1,?)", s.InstallationID)
	if err != nil {
		return err
	}
	var actual string
	if err = s.DB.QueryRowContext(ctx, "SELECT origin_installation_id FROM system_backup_installation_state WHERE singleton_id=1").Scan(&actual); err != nil {
		return err
	}
	if actual != s.InstallationID {
		return errors.New("installation identity mismatch")
	}
	return nil
}

func (s Store) Config(ctx context.Context) (ConfigVersion, error) {
	var out ConfigVersion
	var raw string
	err := s.DB.QueryRowContext(ctx, "SELECT v.version,v.config_json FROM system_backup_config_versions v JOIN system_backup_installation_state i ON i.config_version=v.version WHERE i.singleton_id=1 AND i.origin_installation_id=?", s.InstallationID).Scan(&out.Version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ConfigVersion{Config: DefaultConfig()}, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out.Config)
	return out, err
}

// SaveConfig creates append-only versions. The callback is evaluated under the
// same lock as the comparison, so callers cannot bypass downgrade/auth checks
// with concurrent saves. No credentials or raw provider errors enter events.
func (s Store) SaveConfig(ctx context.Context, expected uint64, actor int, c BackupConfig, authorize func(BackupConfig, BackupConfig) error) (ConfigVersion, error) {
	if actor < 1 {
		return ConfigVersion{}, errors.New("permission_denied")
	}
	if e := c.Validate(); e != nil {
		return ConfigVersion{}, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return ConfigVersion{}, e
	}
	defer tx.Rollback()
	out, e := s.saveConfigTx(ctx, tx, expected, actor, c, authorize)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

func (s Store) saveConfigTx(ctx context.Context, tx *sql.Tx, expected uint64, actor int, c BackupConfig, authorize func(BackupConfig, BackupConfig) error) (ConfigVersion, error) {
	out := ConfigVersion{}
	if actor < 1 {
		return out, errors.New("permission_denied")
	}
	if err := c.Validate(); err != nil {
		return out, err
	}
	var err error
	var version uint64
	var identity string
	if err = tx.QueryRowContext(ctx, "SELECT config_version,origin_installation_id FROM system_backup_installation_state WHERE singleton_id=1 FOR UPDATE").Scan(&version, &identity); err != nil {
		return out, err
	}
	if identity != s.InstallationID {
		return out, errors.New("not_ready")
	}
	if version != expected {
		return out, errors.New("version_conflict")
	}
	before := DefaultConfig()
	if version != 0 {
		var raw string
		if err = tx.QueryRowContext(ctx, "SELECT config_json FROM system_backup_config_versions WHERE version=?", version).Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &before); err != nil {
			return out, err
		}
	}
	if authorize == nil {
		return out, errors.New("permission_denied")
	}
	if err = authorize(before, c); err != nil {
		return out, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO system_backup_config_versions(version,config_json,actor_id) VALUES(?,?,?)", version+1, raw, actor); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_installation_state SET config_version=? WHERE singleton_id=1", version+1); err != nil {
		return out, err
	}
	if err = appendEvent(ctx, tx, "", "config_saved", map[string]any{"actor_id": actor, "old_version": version, "new_version": version + 1}); err != nil {
		return out, err
	}
	return ConfigVersion{version + 1, c}, nil
}

type CreateTaskRequest struct {
	Type     TaskType `json:"task_type"`
	Purpose  string   `json:"task_purpose"`
	SourceID string   `json:"source_public_id,omitempty"`
}

func RequestHash(v any) (string, error) {
	b, e := canonicalJSON(v)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Old installations persisted encoding/json hashes. Accept them only when
// comparing an existing idempotency result; all new requests use RFC 8785.
func requestHashMatches(stored, canonical string, v any) bool {
	if stored == canonical {
		return true
	}
	legacy, err := legacyJSONHash(v)
	return err == nil && stored == legacy
}

// Existing state-evidence identities are immutable; they retain their original
// encoding independently of the newer API/signature canonicalization scheme.
func legacyJSONHash(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func (s Store) Create(ctx context.Context, actor int, key string, r CreateTaskRequest, admit func(context.Context, *sql.Tx, ConfigVersion, CreateTaskRequest) error) (Task, bool, error) {
	var zero Task
	if actor < 1 || !idemPattern.MatchString(key) || normalPaths[r.Type] == nil || (r.Purpose != "acceptance" && r.Purpose != "functional_test") {
		return zero, false, errors.New("validation_failed")
	}
	if r.Type != Backup && !publicIDPattern.MatchString(r.SourceID) {
		return zero, false, errors.New("validation_failed")
	}
	if r.Type == Backup && r.SourceID != "" {
		return zero, false, errors.New("validation_failed")
	}
	hash, err := RequestHash(r)
	if err != nil {
		return zero, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return zero, false, err
	}
	defer tx.Rollback()
	// This installation lock also serializes admission and config changes.
	var version uint64
	var identity string
	if err = tx.QueryRowContext(ctx, "SELECT config_version,origin_installation_id FROM system_backup_installation_state WHERE singleton_id=1 FOR UPDATE").Scan(&version, &identity); err != nil {
		return zero, false, err
	}
	if identity != s.InstallationID {
		return zero, false, errors.New("not_ready")
	}
	var existing, oldHash string
	err = tx.QueryRowContext(ctx, "SELECT public_id,request_hash FROM system_backup_tasks WHERE origin_installation_id=? AND actor_id=? AND task_type=? AND idempotency_key=?", identity, actor, r.Type, key).Scan(&existing, &oldHash)
	if err == nil {
		if !requestHashMatches(oldHash, hash, r) {
			return zero, false, errors.New("idempotency_conflict")
		}
		t, e := getTask(ctx, tx, existing)
		return t, true, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, false, err
	}
	if version == 0 {
		return zero, false, errors.New("not_ready")
	}
	var raw string
	var cfg BackupConfig
	if err = tx.QueryRowContext(ctx, "SELECT config_json FROM system_backup_config_versions WHERE version=?", version).Scan(&raw); err != nil {
		return zero, false, err
	}
	if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
		return zero, false, err
	}
	if !cfg.Enabled || admit == nil {
		return zero, false, errors.New("not_ready")
	}
	if err = admit(ctx, tx, ConfigVersion{version, cfg}, r); err != nil {
		return zero, false, err
	}
	var pending int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_tasks WHERE task_type=? AND status='pending'", r.Type).Scan(&pending); err != nil {
		return zero, false, err
	}
	if pending >= 100 {
		return zero, false, errors.New("rate_limited")
	}
	pendingReason := ""
	if r.Type == Backup {
		var active int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_tasks WHERE task_type='backup' AND status NOT IN ('failed','succeeded','canceled')").Scan(&active); err != nil {
			return zero, false, err
		}
		if active != 0 {
			pendingReason = "global_capture_queue"
		}
	}
	prefix := map[TaskType]string{Backup: "sbk_", Drill: "sdr_", Preflight: "spf_", ArtifactVerify: "sav_"}[r.Type]
	id, err := RandomID(prefix)
	if err != nil {
		return zero, false, err
	}
	request, _ := json.Marshal(r)
	deadline := cfg.TaskDeadlineSeconds
	if r.Type == Preflight {
		deadline = 3600
	}
	if r.Type == ArtifactVerify {
		deadline = 7200
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO system_backup_tasks(public_id,origin_installation_id,task_type,purpose,config_version,config_snapshot,request_json,actor_id,idempotency_key,request_hash,source_public_id,pending_reason,deadline_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)))`, id, identity, r.Type, r.Purpose, version, raw, request, actor, key, hash, r.SourceID, pendingReason, deadline)
	if err != nil {
		return zero, false, err
	}
	if err = appendEvent(ctx, tx, id, "task_created", map[string]any{"actor_id": actor, "config_version": version}); err != nil {
		return zero, false, err
	}
	t, err := getTask(ctx, tx, id)
	if err != nil {
		return zero, false, err
	}
	return t, false, tx.Commit()
}

const taskColumns = "public_id,task_type,purpose,status,row_version,config_version,actor_id,can_cancel,commit_started,artifact_registered,acceptance_eligible,failure_category,pending_reason,attention_reason,created_at,deadline_at,started_at,finished_at,COALESCE(lease_owner,''),lease_until,source_public_id"

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner) (Task, error) {
	var t Task
	e := row.Scan(&t.ID, &t.Type, &t.Purpose, &t.Status, &t.Version, &t.ConfigVersion, &t.ActorID, &t.CanCancel, &t.CommitStarted, &t.Registered, &t.Eligible, &t.Failure, &t.PendingReason, &t.AttentionReason, &t.CreatedAt, &t.DeadlineAt, &t.StartedAt, &t.FinishedAt, &t.LeaseOwner, &t.LeaseUntil, &t.SourceID)
	return t, e
}
func getTask(ctx context.Context, q SQLQueryer, id string) (Task, error) {
	return scanTask(q.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=?", id))
}
func (s Store) Get(ctx context.Context, id string) (Task, error) {
	if !publicIDPattern.MatchString(id) {
		return Task{}, errors.New("not_found")
	}
	return getTask(ctx, s.DB, id)
}
func (s Store) List(ctx context.Context, limit int) ([]Task, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("validation_failed")
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks ORDER BY created_at DESC,public_id DESC LIMIT ?", limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type TaskEvent struct {
	ID        uint64          `json:"id"`
	Type      string          `json:"event_type"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s Store) Events(ctx context.Context, id string, after uint64) ([]TaskEvent, error) {
	if !publicIDPattern.MatchString(id) {
		return nil, errors.New("not_found")
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT id,event_type,details_json,created_at FROM system_backup_events WHERE task_id=? AND id>? ORDER BY id LIMIT 100", id, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []TaskEvent{}
	for rows.Next() {
		var v TaskEvent
		if e = rows.Scan(&v.ID, &v.Type, &v.Details, &v.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func appendEvent(ctx context.Context, tx *sql.Tx, id, kind string, details any) error {
	raw, e := json.Marshal(details)
	if e != nil || len(raw) > 8192 {
		return errors.New("event payload invalid")
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO system_backup_events(task_id,event_type,details_json) VALUES(?,?,?)", id, kind, raw)
	return e
}

// Claim/Advance use database time, ownership and row-version CAS. A stale
// controller cannot advance a task after another controller has taken it.
func (s Store) Claim(ctx context.Context, id, owner string, version uint64, ttl time.Duration) (Task, error) {
	if !refPattern.MatchString(owner) || ttl < 15*time.Second || ttl > 300*time.Second {
		return Task{}, errors.New("validation_failed")
	}
	res, e := s.DB.ExecContext(ctx, `UPDATE system_backup_tasks SET lease_owner=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),row_version=row_version+1 WHERE public_id=? AND row_version=? AND status NOT IN ('succeeded','failed','canceled') AND (lease_owner IS NULL OR lease_owner=? OR lease_until<=UTC_TIMESTAMP(6))`, owner, ttl.Microseconds(), id, version, owner)
	if e != nil {
		return Task{}, e
	}
	if e = requireRow(res, nil); e != nil {
		return Task{}, e
	}
	return s.Get(ctx, id)
}

func (s Store) Advance(ctx context.Context, t Task, owner, next, failure string) (Task, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Task{}, e
	}
	defer tx.Rollback()
	var now time.Time
	if e = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
		return Task{}, e
	}
	updated, e := t.Transition(next, failure, now)
	if e != nil {
		return Task{}, e
	}
	res, e := tx.ExecContext(ctx, `UPDATE system_backup_tasks SET status=?,row_version=?,failure_category=?,can_cancel=?,finished_at=?,attention_reason=?,pending_reason=?,acceptance_eligible=?,started_at=? WHERE public_id=? AND row_version=? AND lease_owner=? AND lease_until>UTC_TIMESTAMP(6)`, updated.Status, updated.Version, updated.Failure, updated.CanCancel, updated.FinishedAt, updated.AttentionReason, updated.PendingReason, updated.Eligible, updated.StartedAt, t.ID, t.Version, owner)
	if e != nil {
		return Task{}, e
	}
	if e = requireRow(res, nil); e != nil {
		return Task{}, e
	}
	if e = appendEvent(ctx, tx, t.ID, "state_changed", map[string]string{"from": t.Status, "to": updated.Status, "failure_category": failure}); e != nil {
		return Task{}, e
	}
	return updated, tx.Commit()
}

func (s Store) Cancel(ctx context.Context, id string, expected uint64, actor int) (Task, error) {
	if actor < 1 {
		return Task{}, errors.New("permission_denied")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Task{}, e
	}
	defer tx.Rollback()
	out, e := s.cancelTx(ctx, tx, id, expected, actor)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

func (s Store) cancelTx(ctx context.Context, tx *sql.Tx, id string, expected uint64, actor int) (Task, error) {
	if actor < 1 {
		return Task{}, errors.New("permission_denied")
	}
	t, e := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=? FOR UPDATE", id))
	if e != nil {
		return t, e
	}
	if t.Version != expected {
		return t, errors.New("version_conflict")
	}
	if t.CommitStarted {
		return t, errors.New("commit_started")
	}
	updated, e := t.Transition("canceling", "none", time.Time{})
	if e != nil {
		return t, e
	}
	res, e := tx.ExecContext(ctx, "UPDATE system_backup_tasks SET status='canceling',can_cancel=0,row_version=row_version+1 WHERE public_id=? AND row_version=?", id, expected)
	if e != nil {
		return t, e
	}
	if e = requireRow(res, nil); e != nil {
		return t, e
	}
	if e = appendEvent(ctx, tx, id, "cancel_requested", map[string]int{"actor_id": actor}); e != nil {
		return t, e
	}
	return updated, nil
}

func PublicError(err error) string {
	if errors.Is(err, ErrFence) {
		return "fencing_token_lost"
	}
	if errors.Is(err, ErrMaintenance) {
		return "not_ready"
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "not_found"
	}
	if err == nil {
		return "none"
	}
	for _, code := range FailureCategories {
		if err.Error() == code {
			return code
		}
	}
	switch err.Error() {
	case "validation_failed", "permission_denied", "not_found", "not_ready", "artifact_unavailable", "version_conflict", "idempotency_conflict", "state_conflict", "commit_started", "rate_limited", "conflict_active_job", "capacity_exceeded", "unsupported_runner_p1", "unsupported_profile_p1", "strong_auth_required", "strong_auth_expired", "strong_auth_invalid_issuer", "strong_auth_invalid", "strong_auth_replayed":
		return err.Error()
	}
	return "internal_error"
}

func (s Store) String() string {
	return fmt.Sprintf("system backup control store (%s)", s.InstallationID)
}
