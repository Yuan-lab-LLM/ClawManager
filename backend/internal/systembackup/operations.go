package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// SQL-only mutations and their replay response commit in one transaction.
// Installation -> operation -> business/control rows is the fixed lock order.
// External effects require a separate durable intent and must not use this.
func idempotentOperation[T any](ctx context.Context, s Store, actor int, endpoint, key string, request any, apply func(*sql.Tx) (T, error), beforeCommit ...func(*sql.Tx) error) (T, bool, error) {
	var zero T
	if actor < 1 || !idemPattern.MatchString(key) || len(endpoint) > 160 || apply == nil {
		return zero, false, errors.New("validation_failed")
	}
	hash, e := RequestHash(request)
	if e != nil {
		return zero, false, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return zero, false, e
	}
	defer tx.Rollback()
	var origin string
	if e = tx.QueryRowContext(ctx, "SELECT origin_installation_id FROM system_backup_installation_state WHERE singleton_id=1 FOR UPDATE").Scan(&origin); e != nil {
		return zero, false, e
	}
	if origin != s.InstallationID {
		return zero, false, errors.New("not_ready")
	}
	var oldHash, status string
	var raw []byte
	e = tx.QueryRowContext(ctx, "SELECT request_hash,status,response_json FROM system_backup_operations WHERE actor_id=? AND endpoint=? AND idempotency_key=? FOR UPDATE", actor, endpoint, key).Scan(&oldHash, &status, &raw)
	if e == nil {
		if !requestHashMatches(oldHash, hash, request) {
			return zero, false, errors.New("idempotency_conflict")
		}
		if status != "succeeded" || len(raw) > 32768 || strictDecode(raw, &zero) != nil {
			return zero, false, errors.New("evidence_incomplete")
		}
		return zero, true, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return zero, false, e
	}
	result, e := apply(tx)
	if e != nil {
		return zero, false, e
	}
	raw, e = json.Marshal(result)
	if e != nil || len(raw) > 32768 {
		return zero, false, errors.New("evidence_incomplete")
	}
	id, e := RandomID("sop_")
	if e != nil {
		return zero, false, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO system_backup_operations(public_id,actor_id,endpoint,idempotency_key,request_hash,status,response_json) VALUES(?,?,?,?,?,'succeeded',?)", id, actor, endpoint, key, hash, raw); e != nil {
		return zero, false, e
	}
	if e = appendEvent(ctx, tx, "", "operation_succeeded", map[string]any{"operation_id": id, "actor_id": actor, "endpoint": endpoint}); e != nil {
		return zero, false, e
	}
	for _, check := range beforeCommit {
		if e = check(tx); e != nil {
			return zero, false, e
		}
	}
	return result, false, tx.Commit()
}

func (s Store) SaveConfigIdempotent(ctx context.Context, expected uint64, actor int, key string, c BackupConfig, authorize func(BackupConfig, BackupConfig) error) (ConfigVersion, bool, error) {
	request := struct {
		Version uint64       `json:"expected_version"`
		Config  BackupConfig `json:"config"`
	}{expected, c}
	return idempotentOperation(ctx, s, actor, "PUT /config", key, request, func(tx *sql.Tx) (ConfigVersion, error) { return s.saveConfigTx(ctx, tx, expected, actor, c, authorize) })
}
func (s Store) CancelIdempotent(ctx context.Context, id string, expected uint64, actor int, key string) (Task, bool, error) {
	if !publicIDPattern.MatchString(id) {
		return Task{}, false, errors.New("validation_failed")
	}
	request := struct {
		Version uint64 `json:"expected_version"`
	}{expected}
	return idempotentOperation(ctx, s, actor, "POST /tasks/"+id+"/cancel", key, request, func(tx *sql.Tx) (Task, error) { return s.cancelTx(ctx, tx, id, expected, actor) })
}
