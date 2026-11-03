package systembackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// FinalWriteRequest persists the exact provider payload before dispatch. It
// contains a versioned destination identity, never a URL or credential envelope.
type FinalWriteRequest struct {
	Type     string    `json:"action_type"`
	Target   SecretRef `json:"target"`
	Key      string    `json:"key"`
	Payload  []byte    `json:"canonical_bytes"`
	SHA256   string    `json:"sha256"`
	Deadline time.Time `json:"dispatch_deadline"`
}
type ExternalAction struct {
	ID        string            `json:"public_id"`
	TaskID    string            `json:"task_id"`
	Type      string            `json:"action_type"`
	State     string            `json:"state"`
	Version   uint64            `json:"row_version"`
	Request   FinalWriteRequest `json:"-"`
	Reference ObjectReference   `json:"reference"`
}

const actionColumns = "public_id,task_id,action_type,state,row_version,request_json,result_json,request_hash"

func scanExternalAction(row rowScanner) (ExternalAction, error) {
	var a ExternalAction
	var raw []byte
	var result sql.NullString
	var hash string
	if err := row.Scan(&a.ID, &a.TaskID, &a.Type, &a.State, &a.Version, &raw, &result, &hash); err != nil {
		return a, err
	}
	if strictDecode(raw, &a.Request) != nil {
		return a, errors.New("evidence_incomplete")
	}
	if actual, err := RequestHash(a.Request); err != nil || actual != hash || a.Type != a.Request.Type {
		return a, errors.New("evidence_incomplete")
	}
	if result.Valid && strictDecode([]byte(result.String), &a.Reference) != nil {
		return a, errors.New("evidence_incomplete")
	}
	return a, nil
}

func (s Store) GetExternalAction(ctx context.Context, taskID, kind string) (ExternalAction, error) {
	return scanExternalAction(s.DB.QueryRowContext(ctx, "SELECT "+actionColumns+" FROM system_backup_external_actions WHERE task_id=? AND action_type=?", taskID, kind))
}

func (r *FinalWriteRequest) validate(origin, id string) error {
	purpose := "artifact"
	suffix := map[string]string{"manifest_put": "manifest.json", "index_put": "index.json", "marker_put": "_COMMITTED", "acceptance_put": "_ACCEPTANCE"}[r.Type]
	key := artifactPrefix(origin, id) + suffix
	if r.Type == "catalog_registration_append" {
		purpose = "catalog"
		key = "registrations/" + origin + "/" + id + ".json"
	}
	if (suffix == "" && r.Type != "catalog_registration_append") || r.Target.Validate(purpose) != nil || r.Key != key || len(r.Payload) == 0 || int64(len(r.Payload)) > MaxImmutableObjectBytes || r.Deadline.IsZero() {
		return errors.New("validation_failed")
	}
	canonical, err := jsoncanonicalBytes(r.Payload)
	if err != nil || !bytes.Equal(canonical, r.Payload) {
		return errors.New("validation_failed")
	}
	h := sha256.Sum256(r.Payload)
	digest := hex.EncodeToString(h[:])
	if r.SHA256 != "" && r.SHA256 != digest {
		return errors.New("checksum_mismatch")
	}
	r.SHA256 = digest
	return nil
}

func jsoncanonicalBytes(raw []byte) ([]byte, error) {
	if err := uniqueJSONKeys(raw); err != nil {
		return nil, err
	}
	return jsoncanonicalizerTransform(raw)
}

// The task lock orders cancel against the first final intent. A single
// unresolved action is additionally enforced by a database unique constraint.
func (s Store) PrepareExternalAction(ctx context.Context, l Leadership, t Task, r FinalWriteRequest) (ExternalAction, Task, error) {
	var zero ExternalAction
	if err := r.validate(s.InstallationID, t.ID); err != nil {
		return zero, t, err
	}
	raw, err := canonicalJSON(r)
	if err != nil {
		return zero, t, err
	}
	hash, _ := RequestHash(r)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return zero, t, err
	}
	defer tx.Rollback()
	if err = s.lockLeadership(ctx, tx, l); err != nil {
		return zero, t, err
	}
	x, err := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=? AND origin_installation_id=? FOR UPDATE", t.ID, s.InstallationID))
	if err != nil {
		return zero, t, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return zero, t, err
	}
	if x.Type != Backup || x.Version != t.Version || x.LeaseOwner != l.Owner || x.LeaseUntil == nil || !x.LeaseUntil.After(now) {
		return zero, t, ErrFence
	}
	old, err := scanExternalAction(tx.QueryRowContext(ctx, "SELECT "+actionColumns+" FROM system_backup_external_actions WHERE task_id=? AND action_type=? FOR UPDATE", t.ID, r.Type))
	if err == nil {
		oldHash, _ := RequestHash(old.Request)
		if oldHash != hash {
			return zero, x, errors.New("artifact_conflict")
		}
		return old, x, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, x, err
	}
	if x.Status != "publishing" || !r.Deadline.Equal(x.DeadlineAt) || !x.DeadlineAt.After(now) {
		return zero, x, errors.New("state_conflict")
	}
	var unresolved int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_external_actions WHERE task_id=? AND state IN ('request_sent','provider_unreachable')", x.ID).Scan(&unresolved); err != nil {
		return zero, x, err
	}
	if unresolved != 0 {
		return zero, x, errors.New("coordination_invalid")
	}
	if (r.Type == "acceptance_put" || r.Type == "catalog_registration_append") && x.Purpose != "acceptance" {
		return zero, x, errors.New("validation_failed")
	}
	if predecessor := map[string]string{"index_put": "manifest_put", "marker_put": "index_put", "acceptance_put": "marker_put", "catalog_registration_append": "acceptance_put"}[r.Type]; predecessor != "" {
		var state string
		if err = tx.QueryRowContext(ctx, "SELECT state FROM system_backup_external_actions WHERE task_id=? AND action_type=?", x.ID, predecessor).Scan(&state); errors.Is(err, sql.ErrNoRows) {
			return zero, x, errors.New("coordination_invalid")
		} else if err != nil {
			return zero, x, err
		}
		if state != "confirmed_present" {
			return zero, x, errors.New("coordination_invalid")
		}
	}
	id, err := RandomID("sea_")
	if err != nil {
		return zero, x, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO system_backup_external_actions(public_id,task_id,action_type,state,request_hash,request_json,dispatch_deadline) VALUES(?,?,?,'request_sent',?,?,?)", id, x.ID, r.Type, hash, raw, r.Deadline); err != nil {
		return zero, x, err
	}
	if !x.CommitStarted {
		if _, err = tx.ExecContext(ctx, "UPDATE system_backup_tasks SET commit_started=1,can_cancel=0,row_version=row_version+1 WHERE public_id=?", x.ID); err != nil {
			return zero, x, err
		}
		x.CommitStarted = true
		x.CanCancel = false
		x.Version++
	}
	if err = appendEvent(ctx, tx, x.ID, "external_action_changed", map[string]string{"action_id": id, "action_type": r.Type, "state": "request_sent", "request_hash": hash}); err != nil {
		return zero, x, err
	}
	a := ExternalAction{ID: id, TaskID: x.ID, Type: r.Type, State: "request_sent", Version: 1, Request: r}
	return a, x, tx.Commit()
}

// confirmed_absent is deliberately unavailable here: elapsed time or S3 404
// alone cannot prove a previously dispatched request can no longer arrive.
func (s Store) ResolveExternalAction(ctx context.Context, l Leadership, t Task, a ExternalAction, state string, ref ObjectReference) error {
	if state != "confirmed_present" && state != "provider_unreachable" && state != "conflict" {
		return errors.New("validation_failed")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.lockLeadership(ctx, tx, l); err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRowContext(ctx, "SELECT row_version=? AND lease_owner=? AND lease_until>UTC_TIMESTAMP(6) FROM system_backup_tasks WHERE public_id=? AND origin_installation_id=? FOR UPDATE", t.Version, l.Owner, t.ID, s.InstallationID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrFence
	}
	x, err := scanExternalAction(tx.QueryRowContext(ctx, "SELECT "+actionColumns+" FROM system_backup_external_actions WHERE public_id=? AND task_id=? FOR UPDATE", a.ID, t.ID))
	if err != nil {
		return err
	}
	if state == "confirmed_present" && (ref.Key != x.Request.Key || ref.Bytes != int64(len(x.Request.Payload)) || ref.SHA256 != x.Request.SHA256 || ref.VersionID == "" || ref.VersionID == "null") {
		return errors.New("artifact_conflict")
	}
	if x.State == "confirmed_present" || x.State == "confirmed_absent" || x.State == "conflict" {
		if x.State == state && (state != "confirmed_present" || x.Reference == ref) {
			return tx.Commit()
		}
		return errors.New("state_conflict")
	}
	var result any
	if state == "confirmed_present" {
		b, e := json.Marshal(ref)
		if e != nil {
			return e
		}
		result = string(b)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_external_actions SET state=?,result_json=?,row_version=row_version+1,updated_at=UTC_TIMESTAMP(6) WHERE public_id=?", state, result, a.ID); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, t.ID, "external_action_changed", map[string]string{"action_id": a.ID, "action_type": a.Type, "state": state}); err != nil {
		return err
	}
	return tx.Commit()
}
