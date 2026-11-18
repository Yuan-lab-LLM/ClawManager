package systembackup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type CoreJobCleanup struct {
	AttemptID string `json:"attempt_id"`
	Phase     string `json:"phase"`
	JobUID    string `json:"job_uid"`
}
type CoreStageCleanup struct {
	ResourceID  string `json:"resource_id"`
	PVCUID      string `json:"pvc_uid"`
	PVUID       string `json:"pv_uid"`
	RequestHash string `json:"request_hash"`
}
type CoreCleanupEvidence struct {
	Schema          string             `json:"schema"`
	TaskID          string             `json:"task_id"`
	ManifestHash    string             `json:"manifest_hash"`
	Jobs            []CoreJobCleanup   `json:"jobs"`
	Stages          []CoreStageCleanup `json:"stages"`
	CompletedAt     time.Time          `json:"completed_at"`
	UploadConfigUID string             `json:"upload_config_uid"`
}
type CorePublication struct {
	TaskID                string
	Manifest              SignedManifest
	ManifestHash          string
	Upload                *UploadJobRequest
	Index                 *SignedIndex
	Cleanup               *CoreCleanupEvidence
	CleanupHash           string
	UploadConfigSent      bool
	UploadConfigUID       string
	UploadConfigDeletedAt *time.Time
}

const corePublicationColumns = "backup_id,signed_manifest_json,manifest_hash,upload_request_json,signed_index_json,cleanup_json,COALESCE(cleanup_hash,''),upload_config_create_sent,upload_config_uid,upload_config_deleted_at"

func scanCorePublication(row rowScanner) (CorePublication, error) {
	var p CorePublication
	var manifest []byte
	var upload, index, cleanup sql.NullString
	var configDeleted sql.NullTime
	if err := row.Scan(&p.TaskID, &manifest, &p.ManifestHash, &upload, &index, &cleanup, &p.CleanupHash, &p.UploadConfigSent, &p.UploadConfigUID, &configDeleted); err != nil {
		return p, err
	}
	if configDeleted.Valid {
		p.UploadConfigDeletedAt = &configDeleted.Time
	}
	if strictDecode(manifest, &p.Manifest) != nil || p.Manifest.Payload.Validate() != nil || p.Manifest.Payload.BackupID != p.TaskID {
		return p, errors.New("evidence_incomplete")
	}
	hash, err := RequestHash(p.Manifest)
	if err != nil || hash != p.ManifestHash {
		return p, errors.New("evidence_incomplete")
	}
	if upload.Valid {
		p.Upload = &UploadJobRequest{}
		if strictDecode([]byte(upload.String), p.Upload) != nil {
			return p, errors.New("evidence_incomplete")
		}
	}
	if index.Valid {
		p.Index = &SignedIndex{}
		if strictDecode([]byte(index.String), p.Index) != nil || ValidateArtifactIndex(p.Index.Payload, p.Manifest.Payload, p.ManifestHash) != nil {
			return p, errors.New("evidence_incomplete")
		}
	}
	if cleanup.Valid {
		p.Cleanup = &CoreCleanupEvidence{}
		if strictDecode([]byte(cleanup.String), p.Cleanup) != nil {
			return p, errors.New("evidence_incomplete")
		}
		hash, err = RequestHash(p.Cleanup)
		if err != nil || hash != p.CleanupHash {
			return p, errors.New("evidence_incomplete")
		}
	}
	return p, nil
}
func (s Store) CorePublication(ctx context.Context, taskID string) (CorePublication, error) {
	return scanCorePublication(s.DB.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=?", taskID))
}
func (s Store) PrepareCorePublication(ctx context.Context, l Leadership, t Task, signed SignedManifest, public ed25519.PublicKey) (CorePublication, error) {
	var zero CorePublication
	if VerifyManifest(signed, public, signed.Signature.KeyVersion) != nil {
		return zero, errors.New("signature_invalid")
	}
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if x.Status != "validating" {
		return zero, errors.New("state_conflict")
	}
	hash, err := RequestHash(signed)
	if err != nil {
		return zero, err
	}
	old, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", x.ID))
	if err == nil {
		if old.ManifestHash != hash {
			return zero, errors.New("artifact_conflict")
		}
		return old, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	plan, err := scanCapturePlan(tx.QueryRowContext(ctx, "SELECT "+capturePlanColumns+" FROM system_backup_capture_plans WHERE task_id=? FOR UPDATE", x.ID))
	if err != nil {
		return zero, err
	}
	if plan.Manifest == nil || plan.Seal == nil || plan.SourceResumedAt == nil || plan.ForcedResumeAt != nil {
		return zero, errors.New("evidence_incomplete")
	}
	actualHash, err := RequestHash(signed.Payload)
	if err != nil || actualHash != plan.Seal.ManifestHash || signed.Payload.OriginInstallationID != s.InstallationID || signed.Payload.Purpose != x.Purpose {
		return zero, errors.New("artifact_conflict")
	}
	var raw []byte
	var cfg BackupConfig
	if err = tx.QueryRowContext(ctx, "SELECT config_snapshot FROM system_backup_tasks WHERE public_id=?", x.ID).Scan(&raw); err != nil {
		return zero, err
	}
	if strictDecode(raw, &cfg) != nil || cfg.Validate() != nil || cfg.SigningKey == nil || cfg.EncryptionKey == nil || signed.Signature.KeyVersion != cfg.SigningKey.Version {
		return zero, errors.New("evidence_incomplete")
	}
	for _, part := range signed.Payload.Parts {
		if part.Key.Reference != *cfg.EncryptionKey {
			return zero, errors.New("encryption_key_unavailable")
		}
	}
	raw, err = canonicalJSON(signed)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO system_backup_artifacts(backup_id,signed_manifest_json,manifest_hash) VALUES(?,?,?)", x.ID, raw, hash); err != nil {
		return zero, err
	}
	if err = appendEvent(ctx, tx, x.ID, "diagnostic.publication.manifest_frozen", map[string]string{"manifest_hash": hash}); err != nil {
		return zero, err
	}
	return CorePublication{TaskID: x.ID, Manifest: signed, ManifestHash: hash}, tx.Commit()
}
func (s Store) FreezeUploadRequest(ctx context.Context, l Leadership, t Task, r UploadJobRequest) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, _, err = uploadJob(Attempt{ID: "sat_00000000000000000000000000000000", TaskID: t.ID, Phase: "upload", Request: raw}); err != nil {
		return err
	}
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.Status != "validating" {
		return errors.New("state_conflict")
	}
	plan, err := scanCapturePlan(tx.QueryRowContext(ctx, "SELECT "+capturePlanColumns+" FROM system_backup_capture_plans WHERE task_id=?", x.ID))
	if err != nil {
		return err
	}
	if plan.SourceResumedAt == nil || plan.ForcedResumeAt != nil || r.Origin != s.InstallationID || r.BackupID != x.ID || !r.DeadlineAt.Equal(x.DeadlineAt) || r.OrdinaryStage != plan.Request.Ordinary.Stage || r.SecretStage != plan.Request.Secrets.Stage || r.Namespace != plan.Request.Ordinary.Namespace || r.NamespaceUID != plan.Request.Ordinary.NamespaceUID || r.Namespace != plan.Request.Secrets.Namespace || r.NamespaceUID != plan.Request.Secrets.NamespaceUID || r.NodeName != plan.Request.Ordinary.NodeName || r.MaxBytes != plan.Request.Ordinary.MaxBytes+plan.Request.Secrets.MaxBytes {
		return errors.New("isolation_invalid")
	}
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", x.ID))
	if err != nil {
		return err
	}
	if core.Upload != nil {
		old, _ := RequestHash(core.Upload)
		hash, _ := RequestHash(r)
		if old != hash {
			return errors.New("artifact_conflict")
		}
		return tx.Commit()
	}
	if core.UploadConfigUID == "" || core.UploadConfigUID != r.OperationSecretUID {
		return errors.New("evidence_incomplete")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET upload_request_json=? WHERE backup_id=?", raw, x.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) captureConfigMutation(ctx context.Context, l Leadership, t Task, uid string, sent bool) error {
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.Status != "validating" && x.Status != "canceling" {
		return errors.New("state_conflict")
	}
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", x.ID))
	if err != nil {
		return err
	}
	if sent {
		if x.Status != "validating" || core.UploadConfigDeletedAt != nil {
			return errors.New("state_conflict")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET upload_config_create_sent=1 WHERE backup_id=?", x.ID); err != nil {
			return err
		}
	} else {
		if !refPattern.MatchString(uid) || core.UploadConfigUID != "" && core.UploadConfigUID != uid {
			return errors.New("artifact_conflict")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET upload_config_uid=? WHERE backup_id=?", uid, x.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s Store) MarkCoreConfigDeleted(ctx context.Context, l Leadership, t Task, uid string) error {
	tx, _, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", t.ID))
	if err != nil {
		return err
	}
	if core.UploadConfigUID != uid || uid == "" && core.UploadConfigSent {
		return errors.New("cleanup_unsafe_target")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET upload_config_deleted_at=COALESCE(upload_config_deleted_at,UTC_TIMESTAMP(6)) WHERE backup_id=?", t.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) FreezeCoreIndex(ctx context.Context, l Leadership, t Task, index SignedIndex, public ed25519.PublicKey) error {
	if verifyObject("clawmanager:artifact-index:v1", index.Payload, index.Signature, public, index.Signature.KeyVersion) != nil {
		return errors.New("signature_invalid")
	}
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.Status != "validating" {
		return errors.New("state_conflict")
	}
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", x.ID))
	if err != nil {
		return err
	}
	if core.Upload == nil || index.Signature.KeyVersion != core.Manifest.Signature.KeyVersion || ValidateArtifactIndex(index.Payload, core.Manifest.Payload, core.ManifestHash) != nil {
		return errors.New("artifact_conflict")
	}
	if VerifyManifest(core.Manifest, public, index.Signature.KeyVersion) != nil {
		return errors.New("signing_key_unavailable")
	}
	a, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE task_id=? AND phase='upload' AND attempt_number=1", x.ID))
	if err != nil {
		return err
	}
	var result UploadJobResult
	if a.Status != "succeeded" || strictDecode(a.Result, &result) != nil || result.Report == nil || result.State != "succeeded" || result.Report.Index.Key != (PartUploadRequest{Origin: s.InstallationID, BackupID: x.ID, AttemptID: a.ID}).indexKey() {
		return errors.New("evidence_incomplete")
	}
	var uid string
	if err = tx.QueryRowContext(ctx, "SELECT job_uid FROM system_backup_capture_jobs WHERE attempt_id=? AND task_id=?", a.ID, x.ID).Scan(&uid); err != nil {
		return err
	}
	if uid != result.JobUID {
		return errors.New("evidence_incomplete")
	}
	if core.Index != nil {
		old, _ := RequestHash(core.Index)
		hash, _ := RequestHash(index)
		if old != hash {
			return errors.New("artifact_conflict")
		}
		return tx.Commit()
	}
	raw, err := canonicalJSON(index)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET signed_index_json=? WHERE backup_id=?", raw, x.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) MarkBackupJobStopped(ctx context.Context, l Leadership, t Task, a Attempt, uid string) error {
	tx, _, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actualUID string
	var stopped sql.NullTime
	if err = tx.QueryRowContext(ctx, "SELECT job_uid,stopped_at FROM system_backup_capture_jobs WHERE attempt_id=? AND task_id=? FOR UPDATE", a.ID, t.ID).Scan(&actualUID, &stopped); err != nil {
		return err
	}
	if uid == "" || actualUID != uid {
		return errors.New("artifact_conflict")
	}
	if stopped.Valid {
		return tx.Commit()
	}
	res, err := tx.ExecContext(ctx, "UPDATE system_backup_capture_jobs SET stopped_at=COALESCE(stopped_at,UTC_TIMESTAMP(6)) WHERE attempt_id=? AND task_id=? AND job_uid=?", a.ID, t.ID, uid)
	if err = requireRow(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) RecordCoreCleanup(ctx context.Context, l Leadership, t Task) (CorePublication, error) {
	var zero CorePublication
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", x.ID))
	if err != nil {
		return zero, err
	}
	if core.Cleanup != nil {
		return core, tx.Commit()
	}
	if core.UploadConfigDeletedAt == nil {
		return zero, errors.New("staging_cleanup_failed")
	}
	if x.Status != "publishing" && x.Status != "finalization_unknown" || core.Index == nil {
		return zero, errors.New("state_conflict")
	}
	evidence := CoreCleanupEvidence{Schema: "system-backup-core-cleanup.v1", TaskID: x.ID, ManifestHash: core.ManifestHash, Jobs: []CoreJobCleanup{}, Stages: []CoreStageCleanup{}, UploadConfigUID: core.UploadConfigUID}
	for _, phase := range []string{"capture", "secret_capture", "upload", "verify"} {
		var item CoreJobCleanup
		var stopped sql.NullTime
		err = tx.QueryRowContext(ctx, "SELECT j.attempt_id,a.phase,j.job_uid,j.stopped_at FROM system_backup_capture_jobs j JOIN system_backup_attempts a ON a.public_id=j.attempt_id WHERE a.task_id=? AND a.phase=? AND a.attempt_number=1", x.ID, phase).Scan(&item.AttemptID, &item.Phase, &item.JobUID, &stopped)
		if err != nil {
			return zero, err
		}
		if !stopped.Valid {
			return zero, errors.New("staging_cleanup_failed")
		}
		evidence.Jobs = append(evidence.Jobs, item)
	}
	for _, phase := range []string{"capture", "secret_capture"} {
		v, e := scanStageVolume(tx.QueryRowContext(ctx, "SELECT "+stageVolumeColumns+" FROM system_backup_stage_resources WHERE task_id=? AND phase=?", x.ID, phase))
		if e != nil {
			return zero, e
		}
		if v.State != "deleted" || v.PVCUID == "" || v.PVUID == "" {
			return zero, errors.New("staging_cleanup_failed")
		}
		evidence.Stages = append(evidence.Stages, CoreStageCleanup{ResourceID: v.ID, PVCUID: v.PVCUID, PVUID: v.PVUID, RequestHash: v.Hash})
	}
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&evidence.CompletedAt); err != nil {
		return zero, err
	}
	raw, err := canonicalJSON(evidence)
	if err != nil {
		return zero, err
	}
	hash, err := RequestHash(evidence)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET cleanup_json=?,cleanup_hash=? WHERE backup_id=?", raw, hash, x.ID); err != nil {
		return zero, err
	}
	core.Cleanup = &evidence
	core.CleanupHash = hash
	return core, tx.Commit()
}

func (s Store) MarkCoreCommitted(ctx context.Context, l Leadership, t Task) error {
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.Status != "publishing" && x.Status != "finalization_unknown" {
		return errors.New("state_conflict")
	}
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=? FOR UPDATE", x.ID))
	if err != nil {
		return err
	}
	if core.Index == nil {
		return errors.New("evidence_incomplete")
	}
	refs := map[string]ObjectReference{}
	for _, kind := range []string{"manifest_put", "index_put", "marker_put"} {
		a, e := scanExternalAction(tx.QueryRowContext(ctx, "SELECT "+actionColumns+" FROM system_backup_external_actions WHERE task_id=? AND action_type=?", x.ID, kind))
		if e != nil {
			return e
		}
		if a.State != "confirmed_present" {
			return errors.New("evidence_incomplete")
		}
		refs[kind] = a.Reference
	}
	indexHash, e := RequestHash(core.Index)
	if e != nil || refs["manifest_put"].SHA256 != core.ManifestHash || refs["index_put"].SHA256 != indexHash {
		return errors.New("artifact_conflict")
	}
	marker, err := scanExternalAction(tx.QueryRowContext(ctx, "SELECT "+actionColumns+" FROM system_backup_external_actions WHERE task_id=? AND action_type='marker_put'", x.ID))
	if err != nil {
		return err
	}
	var signed SignedCommit
	if strictDecode(marker.Request.Payload, &signed) != nil || signed.Payload.Schema != "system-backup-commit.v1" || signed.Payload.BackupID != x.ID || signed.Payload.OriginInstallationID != s.InstallationID || signed.Payload.Manifest != refs["manifest_put"] || signed.Payload.Index != refs["index_put"] {
		return errors.New("artifact_conflict")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET artifact_state='committed' WHERE backup_id=?", x.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) CompleteFunctionalBackup(ctx context.Context, l Leadership, t Task) error {
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.Purpose != "functional_test" || x.Status != "publishing" && x.Status != "finalization_unknown" || !x.CommitStarted || x.Registered || x.Eligible {
		return errors.New("state_conflict")
	}
	core, err := scanCorePublication(tx.QueryRowContext(ctx, "SELECT "+corePublicationColumns+" FROM system_backup_artifacts WHERE backup_id=?", x.ID))
	if err != nil {
		return err
	}
	if core.Cleanup == nil || core.Index == nil {
		return errors.New("staging_cleanup_failed")
	}
	refs := map[string]ObjectReference{}
	for _, kind := range []string{"manifest_put", "index_put", "marker_put"} {
		a, e := scanExternalAction(tx.QueryRowContext(ctx, "SELECT "+actionColumns+" FROM system_backup_external_actions WHERE task_id=? AND action_type=?", x.ID, kind))
		if e != nil {
			return e
		}
		if a.State != "confirmed_present" {
			return errors.New("evidence_incomplete")
		}
		refs[kind] = a.Reference
	}
	a, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE task_id=? AND phase='verify' AND attempt_number=1", x.ID))
	indexHash, e := RequestHash(core.Index)
	if e != nil || refs["manifest_put"].SHA256 != core.ManifestHash || refs["index_put"].SHA256 != indexHash {
		return errors.New("artifact_conflict")
	}
	if err != nil {
		return err
	}
	var verified VerifyJobResult
	if a.Status != "succeeded" || strictDecode(a.Result, &verified) != nil || verified.Report == nil || !verified.Report.IntegrityVerified || !verified.Report.SnapshotValidated || verified.Report.AcceptanceEligible || verified.Report.BackupID != x.ID || verified.Report.Origin != s.InstallationID || verified.Report.ManifestSHA256 != refs["manifest_put"].SHA256 || verified.Report.IndexSHA256 != refs["index_put"].SHA256 || verified.Report.CommitSHA256 != refs["marker_put"].SHA256 {
		return errors.New("evidence_incomplete")
	}
	if verified.State != "succeeded" || verified.Report.PartsVerified != len(RequiredParts) {
		return errors.New("evidence_incomplete")
	}
	for _, job := range core.Cleanup.Jobs {
		if job.Phase == "verify" && job.JobUID != verified.JobUID {
			return errors.New("evidence_incomplete")
		}
	}
	plan, err := scanCapturePlan(tx.QueryRowContext(ctx, "SELECT "+capturePlanColumns+" FROM system_backup_capture_plans WHERE task_id=? FOR UPDATE", x.ID))
	if err != nil {
		return err
	}
	if plan.ForcedResumeAt != nil || plan.SourceResumedAt == nil {
		return errors.New("quiesce_resume_failed")
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_artifacts SET artifact_state='committed',artifact_usable=1 WHERE backup_id=?", x.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_tasks SET status='succeeded',failure_category='none',can_cancel=0,acceptance_eligible=0,artifact_registered=0,finished_at=?,row_version=row_version+1,pending_reason='',attention_reason='' WHERE public_id=?", now, x.ID); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, x.ID, "state_changed", map[string]string{"from": x.Status, "to": "succeeded", "failure_category": "none"}); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, x.ID, "audit.functional_completed", map[string]any{"actor_id": x.ActorID, "acceptance_eligible": false, "manifest_hash": core.ManifestHash, "cleanup_hash": core.CleanupHash, "verifier_job_uid": verified.JobUID}); err != nil {
		return err
	}
	return tx.Commit()
}
