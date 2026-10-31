package systembackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"clawreef/internal/systembackup/team"
)

type StateEvidence struct {
	RunID              string   `json:"run_id"`
	SourceBackupID     string   `json:"source_backup_id"`
	TargetIdentityHash string   `json:"target_identity_hash"`
	Table              string   `json:"table"`
	EntityKeyHash      string   `json:"entity_key_hash"`
	SourceHash         string   `json:"source_hash"`
	RestoredHash       string   `json:"restored_hash"`
	BeforeState        team.Row `json:"before_state"`
	AfterState         team.Row `json:"after_state"`
	ChangedFields      []string `json:"changed_fields"`
	Reason             string   `json:"reason"`
	Quarantined        bool     `json:"quarantined"`
}
type encryptedEvidence struct {
	Key     WrappedKey `json:"key"`
	Payload []byte     `json:"payload"`
}

// RecordRestoreEvidence commits immutable original-state references before
// target writes. Large payloads/results are retained in the source artifact;
// this bounded journal records hashes and state changes, never duplicate raw
// task contents or credentials. It lives in the controller DB, not the target.
func (s Store) RecordRestoreEvidence(ctx context.Context, runID, sourceID, targetHash string, table SQLTable, change team.Change, wrapper DataKeyWrapper) (StateEvidence, error) {
	var out StateEvidence
	if len(runID) != 36 || runID[:4] != "sdr_" || !publicIDPattern.MatchString(runID) || !publicIDPattern.MatchString(sourceID) || sourceID[:4] != "sbk_" || !validHash(targetHash) || table.Name != change.Table || wrapper == nil {
		return out, errors.New("invalid evidence identity")
	}
	keys := team.Row{}
	for _, key := range table.PrimaryKey {
		v, ok := change.Before[key]
		if !ok || v.IsNull {
			return out, errors.New("missing evidence primary key")
		}
		keys[key] = v
	}
	if len(keys) == 0 {
		return out, errors.New("missing evidence identity")
	}
	keyHash, e := legacyJSONHash(keys)
	if e != nil {
		return out, e
	}
	beforeHash, e := legacyJSONHash(change.Before)
	if e != nil {
		return out, e
	}
	afterHash, e := legacyJSONHash(change.After)
	if e != nil {
		return out, e
	}
	out = StateEvidence{RunID: runID, SourceBackupID: sourceID, TargetIdentityHash: targetHash, Table: table.Name, EntityKeyHash: keyHash, SourceHash: beforeHash, RestoredHash: afterHash, BeforeState: team.Row{}, AfterState: team.Row{}, ChangedFields: []string{}, Reason: change.Reason, Quarantined: change.Quarantined}
	for _, column := range table.Columns {
		before, after := change.Before[column.Name], change.After[column.Name]
		if !reflect.DeepEqual(before, after) {
			out.ChangedFields = append(out.ChangedFields, column.Name)
		}
		switch column.Name {
		case "status", "state", "workflow_state", "availability", "finished_at":
			out.BeforeState[column.Name] = before
			out.AfterState[column.Name] = after
		}
	}
	raw, e := json.Marshal(out)
	if e != nil || len(raw) > 6000 {
		return out, errors.New("evidence_incomplete")
	}
	identity := []byte(runID + "\x00" + targetHash + "\x00" + table.Name + "\x00" + keyHash)
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		return out, e
	}
	defer clear(key)
	wrapped, e := wrapper.Wrap(ctx, key, identity)
	if e != nil {
		return out, e
	}
	var cipher bytes.Buffer
	if e = Encrypt(ctx, &cipher, bytes.NewReader(raw), key, identity); e != nil {
		return out, e
	}
	payload, e := json.Marshal(encryptedEvidence{wrapped, cipher.Bytes()})
	if e != nil || len(payload) > 8192 {
		return out, errors.New("evidence_incomplete")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_restore_evidence(run_id,entity_type,entity_key_hash,source_hash,encrypted_payload) VALUES(?,?,?,?,?)", runID, table.Name, keyHash, beforeHash, payload)
	if e != nil {
		return out, e
	}
	var storedHash string
	var stored []byte
	if e = tx.QueryRowContext(ctx, "SELECT source_hash,encrypted_payload FROM system_backup_restore_evidence WHERE run_id=? AND entity_type=? AND entity_key_hash=? FOR UPDATE", runID, table.Name, keyHash).Scan(&storedHash, &stored); e != nil {
		return out, e
	}
	if storedHash != beforeHash {
		return out, errors.New("original restore evidence conflict")
	}
	original, e := decodeStateEvidence(ctx, stored, identity, wrapper)
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(out, original) {
		return out, errors.New("restore policy or target identity changed")
	}
	if change.Quarantined {
		if _, e = tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_quarantine(target_identity_hash,entity_type,entity_key_hash,run_id,reason) VALUES(?,?,?,?,?)", targetHash, table.Name, keyHash, runID, change.Reason); e != nil {
			return out, e
		}
	}
	return original, tx.Commit()
}

func decodeStateEvidence(ctx context.Context, payload, identity []byte, w DataKeyWrapper) (StateEvidence, error) {
	var out StateEvidence
	var envelope encryptedEvidence
	if e := json.Unmarshal(payload, &envelope); e != nil {
		return out, e
	}
	key, e := w.Unwrap(ctx, envelope.Key, identity)
	if e != nil {
		return out, e
	}
	defer clear(key)
	var plain bytes.Buffer
	if e = Decrypt(ctx, &plain, bytes.NewReader(envelope.Payload), key, identity); e != nil {
		return out, e
	}
	e = json.Unmarshal(plain.Bytes(), &out)
	clear(plain.Bytes())
	return out, e
}

func (s Store) IsQuarantined(ctx context.Context, targetHash, table, keyHash string) (bool, error) {
	if !validHash(targetHash) || !validHash(keyHash) {
		return true, errors.New("invalid quarantine identity")
	}
	var n int
	e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_quarantine WHERE target_identity_hash=? AND entity_type=? AND entity_key_hash=?", targetHash, table, keyHash).Scan(&n)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return n > 0, e
}
