package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/go-sql-driver/mysql"
	batchv1 "k8s.io/api/batch/v1"
)

type CapturePlanRequest struct {
	Ordinary           CaptureJobRequest `json:"capture"`
	Secrets            CaptureJobRequest `json:"secret_capture"`
	WriterRegistryHash string            `json:"writer_registry_hash"`
}
type CaptureSeal struct {
	Schema             string        `json:"schema"`
	TaskID             string        `json:"task_id"`
	Gate               GateOwnership `json:"gate"`
	WriterRegistryHash string        `json:"writer_registry_hash"`
	ManifestHash       string        `json:"manifest_hash"`
	CaptureAttemptID   string        `json:"capture_attempt_id"`
	SecretAttemptID    string        `json:"secret_capture_attempt_id"`
	CaptureJobUID      string        `json:"capture_job_uid"`
	SecretJobUID       string        `json:"secret_capture_job_uid"`
	SealedAt           time.Time     `json:"sealed_at"`
}
type CapturePlan struct {
	TaskID          string
	Request         CapturePlanRequest
	Manifest        *Manifest
	Seal            *CaptureSeal
	SourceResumedAt *time.Time
	ForcedResumeAt  *time.Time
}
type CaptureJobBinding struct {
	AttemptID, TaskID, Namespace, Name, UID string
	Ready                                   bool
	ObservedAt                              time.Time
}

func scanCapturePlan(row rowScanner) (CapturePlan, error) {
	var p CapturePlan
	var request []byte
	var manifest, seal sql.NullString
	var resumed, forced sql.NullTime
	if err := row.Scan(&p.TaskID, &request, &manifest, &seal, &resumed, &forced); err != nil {
		return p, err
	}
	if strictDecode(request, &p.Request) != nil {
		return p, errors.New("evidence_incomplete")
	}
	if manifest.Valid {
		p.Manifest = &Manifest{}
		if strictDecode([]byte(manifest.String), p.Manifest) != nil || p.Manifest.Validate() != nil {
			return p, errors.New("evidence_incomplete")
		}
	}
	if seal.Valid {
		p.Seal = &CaptureSeal{}
		if strictDecode([]byte(seal.String), p.Seal) != nil {
			return p, errors.New("evidence_incomplete")
		}
	}
	if resumed.Valid {
		p.SourceResumedAt = &resumed.Time
	}
	if forced.Valid {
		p.ForcedResumeAt = &forced.Time
	}
	return p, nil
}

const capturePlanColumns = "task_id,request_json,manifest_json,seal_json,source_resumed_at,forced_resume_at"

func (s Store) GetCapturePlan(ctx context.Context, id string) (CapturePlan, error) {
	return scanCapturePlan(s.DB.QueryRowContext(ctx, "SELECT "+capturePlanColumns+" FROM system_backup_capture_plans WHERE task_id=?", id))
}

func (s Store) beginCaptureMutation(ctx context.Context, l Leadership, t Task) (*sql.Tx, Task, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, t, err
	}
	fail := func(err error) (*sql.Tx, Task, error) { tx.Rollback(); return nil, t, err }
	if err = s.lockLeadership(ctx, tx, l); err != nil {
		return fail(err)
	}
	x, err := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=? AND origin_installation_id=? FOR UPDATE", t.ID, s.InstallationID))
	if err != nil {
		return fail(err)
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return fail(err)
	}
	if x.Type != Backup || x.Version != t.Version || x.LeaseOwner != l.Owner || x.LeaseUntil == nil || !x.LeaseUntil.After(now) {
		return fail(ErrFence)
	}
	return tx, x, nil
}

func (s Store) PrepareCapturePlan(ctx context.Context, l Leadership, t Task, r CapturePlanRequest) (CapturePlan, error) {
	var zero CapturePlan
	if r.Ordinary.Phase != "capture" || r.Secrets.Phase != "secret_capture" || r.Ordinary.Gate != r.Secrets.Gate || r.Ordinary.Origin != s.InstallationID || r.Secrets.Origin != s.InstallationID || r.Ordinary.BackupID != t.ID || r.Secrets.BackupID != t.ID || r.Ordinary.Stage.UID == r.Secrets.Stage.UID || !validHash(r.WriterRegistryHash) || !r.Ordinary.DeadlineAt.Equal(t.DeadlineAt) || !r.Secrets.DeadlineAt.Equal(t.DeadlineAt) {
		return zero, errors.New("validation_failed")
	}
	for _, job := range []CaptureJobRequest{r.Ordinary, r.Secrets} {
		raw, _ := json.Marshal(job)
		if _, _, err := captureJob(Attempt{ID: "sat_00000000000000000000000000000000", TaskID: t.ID, Phase: job.Phase, Request: raw}); err != nil {
			return zero, err
		}
	}
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	hash, err := RequestHash(r)
	if err != nil {
		return zero, err
	}
	old, err := scanCapturePlan(tx.QueryRowContext(ctx, "SELECT "+capturePlanColumns+" FROM system_backup_capture_plans WHERE task_id=? FOR UPDATE", x.ID))
	if err == nil {
		oldHash, _ := RequestHash(old.Request)
		if oldHash != hash {
			return zero, errors.New("artifact_conflict")
		}
		return old, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	if x.Status != "preparing" {
		return zero, errors.New("state_conflict")
	}
	var configRaw []byte
	var config BackupConfig
	if err = tx.QueryRowContext(ctx, "SELECT config_snapshot FROM system_backup_tasks WHERE public_id=?", x.ID).Scan(&configRaw); err != nil {
		return zero, err
	}
	if strictDecode(configRaw, &config) != nil || config.Validate() != nil || r.Ordinary.MaxBytes+r.Secrets.MaxBytes > config.MaxArtifactBytes || r.Ordinary.MaxItems > int(config.MaxSourceItems) || r.Secrets.MaxItems > int(config.MaxSourceItems) {
		return zero, errors.New("validation_failed")
	}
	var writerHash string
	if err = tx.QueryRowContext(ctx, "SELECT evidence_hash FROM system_backup_dependency_health WHERE config_version=? AND dependency='writer_registry' AND status='healthy' AND expires_at>UTC_TIMESTAMP(6)", x.ConfigVersion).Scan(&writerHash); errors.Is(err, sql.ErrNoRows) {
		return zero, errors.New("not_ready")
	} else if err != nil {
		return zero, err
	}
	if writerHash != r.WriterRegistryHash {
		return zero, errors.New("writer_registry_drift")
	}
	var phase string
	var generation, token uint64
	if err = tx.QueryRowContext(ctx, "SELECT phase,generation,fencing_token FROM system_backup_gate WHERE scope=? FOR SHARE NOWAIT", r.Ordinary.Gate.Scope).Scan(&phase, &generation, &token); err != nil {
		var busy *mysql.MySQLError
		if errors.As(err, &busy) && busy.Number == 3572 {
			return zero, ErrMaintenance
		}
		return zero, err
	}
	if phase != "idle" || generation+1 != r.Ordinary.Gate.Generation || token+1 != r.Ordinary.Gate.Token {
		return zero, ErrMaintenance
	}
	raw, err := canonicalJSON(r)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO system_backup_capture_plans(task_id,scope,generation,fencing_token,request_hash,request_json) VALUES(?,?,?,?,?,?)", x.ID, r.Ordinary.Gate.Scope, r.Ordinary.Gate.Generation, r.Ordinary.Gate.Token, hash, raw); err != nil {
		return zero, err
	}
	if err = appendEvent(ctx, tx, x.ID, "diagnostic.capture.plan_frozen", map[string]string{"request_hash": hash}); err != nil {
		return zero, err
	}
	return CapturePlan{TaskID: x.ID, Request: r}, tx.Commit()
}

func (s Store) CaptureBinding(ctx context.Context, attemptID string) (CaptureJobBinding, error) {
	var b CaptureJobBinding
	err := s.DB.QueryRowContext(ctx, "SELECT attempt_id,task_id,namespace_name,job_name,job_uid,ready,observed_at FROM system_backup_capture_jobs WHERE attempt_id=?", attemptID).Scan(&b.AttemptID, &b.TaskID, &b.Namespace, &b.Name, &b.UID, &b.Ready, &b.ObservedAt)
	return b, err
}
func (s Store) BindCaptureJob(ctx context.Context, l Leadership, t Task, a Attempt, job *batchv1.Job) error {
	want, _, err := captureJob(a)
	if err != nil {
		return err
	}
	return s.bindFrozenBackupJob(ctx, l, t, a, job, want, []string{"preparing", "canceling"})
}

func (s Store) BindUploadJob(ctx context.Context, l Leadership, t Task, a Attempt, job *batchv1.Job) error {
	want, _, err := uploadJob(a)
	if err != nil {
		return err
	}
	return s.bindFrozenBackupJob(ctx, l, t, a, job, want, []string{"validating", "canceling"})
}

func (s Store) BindBackupVerifyJob(ctx context.Context, l Leadership, t Task, a Attempt, job *batchv1.Job) error {
	if a.Phase != "verify" {
		return errors.New("validation_failed")
	}
	want, _, err := verifyJob(a)
	if err != nil {
		return err
	}
	return s.bindFrozenBackupJob(ctx, l, t, a, job, want, []string{"publishing", "finalization_unknown", "canceling"})
}

func (s Store) bindFrozenBackupJob(ctx context.Context, l Leadership, t Task, a Attempt, job, want *batchv1.Job, states []string) error {
	if job == nil || job.UID == "" {
		return errors.New("validation_failed")
	}
	valid := matchesCaptureJob(want, job)
	if a.Phase == "verify" {
		valid = matchesVerifyJob(want, job)
	}
	if !valid {
		return errors.New("artifact_conflict")
	}
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !slices.Contains(states, x.Status) || a.TaskID != x.ID {
		return errors.New("state_conflict")
	}
	stored, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE public_id=? AND task_id=? FOR UPDATE", a.ID, x.ID))
	if err != nil {
		return err
	}
	if stored.Status != "running" || stored.Phase != a.Phase || string(stored.Request) != string(a.Request) {
		return errors.New("artifact_conflict")
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_capture_jobs(attempt_id,task_id,namespace_name,job_name,job_uid) VALUES(?,?,?,?,?)", a.ID, x.ID, job.Namespace, job.Name, string(job.UID)); err != nil {
		return err
	}
	var uid string
	if err = tx.QueryRowContext(ctx, "SELECT job_uid FROM system_backup_capture_jobs WHERE attempt_id=? AND task_id=?", a.ID, x.ID).Scan(&uid); err != nil {
		return err
	}
	if uid != string(job.UID) {
		return errors.New("artifact_conflict")
	}
	return tx.Commit()
}
func (s Store) RecordCaptureReady(ctx context.Context, l Leadership, t Task, a Attempt, b CaptureJobBinding, ready bool) error {
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.Status != "preparing" || a.TaskID != x.ID || b.TaskID != x.ID || b.AttemptID != a.ID {
		return errors.New("state_conflict")
	}
	res, err := tx.ExecContext(ctx, "UPDATE system_backup_capture_jobs SET ready=?,observed_at=UTC_TIMESTAMP(6) WHERE attempt_id=? AND task_id=? AND job_uid=?", ready, a.ID, x.ID, b.UID)
	if err = requireRow(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

// Seal is persisted while holding the live gate row. Both original Job UIDs
// and both independent successful phase results must already be durable.
func (s Store) SealCapturePlan(ctx context.Context, l Leadership, t Task) (CapturePlan, error) {
	var zero CapturePlan
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	p, err := scanCapturePlan(tx.QueryRowContext(ctx, "SELECT "+capturePlanColumns+" FROM system_backup_capture_plans WHERE task_id=? FOR UPDATE", x.ID))
	if err != nil {
		return zero, err
	}
	if p.Manifest != nil {
		if p.ForcedResumeAt != nil {
			return zero, errors.New("quiesce_resume_failed")
		}
		return p, tx.Commit()
	}
	if x.Status != "capturing" {
		return zero, errors.New("state_conflict")
	}
	var gatePhase string
	var generation, token uint64
	var owner string
	if err = tx.QueryRowContext(ctx, "SELECT phase,COALESCE(owner,''),generation,fencing_token FROM system_backup_gate WHERE scope=? FOR UPDATE", p.Request.Ordinary.Gate.Scope).Scan(&gatePhase, &owner, &generation, &token); err != nil {
		return zero, err
	}
	gate := p.Request.Ordinary.Gate
	if gatePhase != "held" || owner != gate.Owner || generation != gate.Generation || token != gate.Token {
		return zero, ErrFence
	}
	var candidates []CaptureCandidate
	var attempts []Attempt
	var uids []string
	for _, phase := range []string{"capture", "secret_capture"} {
		a, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE task_id=? AND phase=? AND attempt_number=1", x.ID, phase))
		if e != nil {
			return zero, e
		}
		var result CaptureJobResult
		if a.Status != "sealed" || strictDecode(a.Result, &result) != nil || result.State != "sealed" || result.Candidate == nil {
			return zero, errors.New("evidence_incomplete")
		}
		_, request, e := captureJob(a)
		if e != nil {
			return zero, e
		}
		c := result.Candidate
		if c.AttemptID != a.ID || c.Phase != phase || c.BackupID != x.ID || c.Origin != s.InstallationID || c.Gate != request.Gate || c.StageUID != request.Stage.UID {
			return zero, errors.New("evidence_incomplete")
		}
		var uid string
		if e = tx.QueryRowContext(ctx, "SELECT job_uid FROM system_backup_capture_jobs WHERE attempt_id=? AND task_id=?", a.ID, x.ID).Scan(&uid); e != nil {
			return zero, e
		}
		if uid != result.JobUID {
			return zero, errors.New("evidence_incomplete")
		}
		candidates = append(candidates, *result.Candidate)
		attempts = append(attempts, a)
		uids = append(uids, uid)
	}
	guard := SQLCaptureGuard{Gate: Gate{DB: s.DB}, Ownership: gate}
	manifest, err := AggregateCapturedPhases(ctx, candidates[0], candidates[1], CaptureBudget{MaxBytes: p.Request.Ordinary.MaxBytes + p.Request.Secrets.MaxBytes, MaxItems: p.Request.Ordinary.MaxItems}, guard)
	if err != nil {
		return zero, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return zero, err
	}
	manifest.Purpose = x.Purpose
	hash, err := RequestHash(manifest)
	if err != nil {
		return zero, err
	}
	seal := CaptureSeal{Schema: "system-backup-capture-seal.v1", TaskID: x.ID, Gate: gate, WriterRegistryHash: p.Request.WriterRegistryHash, ManifestHash: hash, CaptureAttemptID: attempts[0].ID, SecretAttemptID: attempts[1].ID, CaptureJobUID: uids[0], SecretJobUID: uids[1], SealedAt: now}
	manifestRaw, _ := canonicalJSON(manifest)
	sealRaw, _ := canonicalJSON(seal)
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_capture_plans SET manifest_json=?,seal_json=? WHERE task_id=?", manifestRaw, sealRaw, x.ID); err != nil {
		return zero, err
	}
	if err = appendEvent(ctx, tx, x.ID, "diagnostic.capture.sealed", map[string]string{"manifest_hash": hash}); err != nil {
		return zero, err
	}
	p.Manifest = &manifest
	p.Seal = &seal
	return p, tx.Commit()
}
