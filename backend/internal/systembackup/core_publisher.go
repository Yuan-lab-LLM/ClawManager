package systembackup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"slices"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type UploadJobDriver interface {
	Ensure(context.Context, Attempt, string, bool) (*batchv1.Job, error)
	Existing(context.Context, Attempt) (*batchv1.Job, error)
	Observe(context.Context, Attempt, string) (UploadJobResult, error)
	Stop(context.Context, Attempt, string) (bool, error)
}
type SnapshotJobDriver interface {
	EnsureBound(context.Context, Attempt, string, bool) (*batchv1.Job, error)
	Existing(context.Context, Attempt) (*batchv1.Job, error)
	ObserveBound(context.Context, Attempt, string) (VerifyJobResult, error)
	StopBound(context.Context, Attempt, string) (bool, error)
}
type CoreArtifactReader interface {
	Read(context.Context, ObjectReference) ([]byte, error)
}
type AcceptancePublicationDriver interface {
	Tick(context.Context, Store, Leadership, Task, CorePublication) error
}
type CorePublisher struct {
	CaptureJobs    CaptureJobDriver
	Uploads        UploadJobDriver
	Verifier       SnapshotJobDriver
	VerifyTemplate VerifyJobRequest
	Resources      CoreResourceDriver
	ResolveReader  func(context.Context, SecretRef) (CoreArtifactReader, error)
	ResolveSigner  func(context.Context, SecretRef) (ed25519.PrivateKey, error)
	FinalWrites    FinalWriteDriver
	Acceptance     AcceptancePublicationDriver
}

func (r CorePublisher) ReadyForPurpose(purpose string) error {
	if purpose == "acceptance" && r.Acceptance == nil {
		return errors.New("not_ready")
	}
	if purpose != "acceptance" && purpose != "functional_test" {
		return errors.New("validation_failed")
	}
	return nil
}

func (s Store) frozenBackupConfig(ctx context.Context, t Task) (BackupConfig, error) {
	var c BackupConfig
	var raw []byte
	if err := s.DB.QueryRowContext(ctx, "SELECT config_snapshot FROM system_backup_tasks WHERE public_id=? AND origin_installation_id=?", t.ID, s.InstallationID).Scan(&raw); err != nil {
		return c, err
	}
	if strictDecode(raw, &c) != nil || c.Validate() != nil {
		return c, errors.New("evidence_incomplete")
	}
	return c, nil
}

func (r CorePublisher) Tick(ctx context.Context, s Store, l Leadership, t Task, capture CapturePlan, stages CaptureResourceDriver) error {
	if r.Uploads == nil || r.Verifier == nil || r.CaptureJobs == nil || r.Resources == nil || r.ResolveReader == nil || r.ResolveSigner == nil || r.FinalWrites.Resolve == nil || stages == nil {
		return errors.New("not_ready")
	}
	// Formal publication is unavailable until its independent evidence and
	// purpose-specific signing/catalog provider has been installed. The core
	// pipeline must never synthesize acceptance from its functional checks.
	if t.Purpose == "acceptance" && r.Acceptance == nil {
		return errors.New("not_ready")
	}
	config, err := s.frozenBackupConfig(ctx, t)
	if err != nil {
		return err
	}
	if config.SigningKey == nil || config.ArtifactTarget == nil {
		return errors.New("not_ready")
	}
	core, err := s.CorePublication(ctx, t.ID)
	if errors.Is(err, sql.ErrNoRows) {
		if capture.Manifest == nil || capture.SourceResumedAt == nil || capture.ForcedResumeAt != nil {
			return errors.New("evidence_incomplete")
		}
		key, e := r.ResolveSigner(ctx, *config.SigningKey)
		if e != nil {
			return e
		}
		defer clear(key)
		manifest, e := SignManifest(*capture.Manifest, key, config.SigningKey.Version)
		if e != nil {
			return e
		}
		_, e = s.PrepareCorePublication(ctx, l, t, manifest, key.Public().(ed25519.PublicKey))
		return e
	}
	if err != nil {
		return err
	}
	if t.Status == "validating" {
		if core.Upload == nil {
			request, e := r.Resources.PrepareUpload(ctx, s, l, t, capture, core)
			if e != nil {
				return e
			}
			return s.FreezeUploadRequest(ctx, l, t, request)
		}
		a, e := s.GetAttempt(ctx, t.ID, "upload")
		if errors.Is(e, sql.ErrNoRows) {
			_, e = s.PrepareAttempt(ctx, l, t, "upload", *core.Upload)
			return e
		}
		if e != nil {
			return e
		}
		if a.Status == "failed" || a.Status == "abandoned" {
			return s.requestCaptureAbort(ctx, l, t, "runner_failed")
		}
		if a.Status == "running" {
			binding, e := s.ensureBackupExecution(ctx, l, t, a,
				func(step context.Context) (*batchv1.Job, error) { return r.Uploads.Ensure(step, a, "", true) },
				func(step context.Context) (*batchv1.Job, error) { return r.Uploads.Existing(step, a) },
				func(step context.Context, job *batchv1.Job) error { return s.BindUploadJob(step, l, t, a, job) })
			if e != nil {
				return e
			}
			result, e := r.Uploads.Observe(ctx, a, binding.UID)
			if e != nil {
				return e
			}
			if result.State == "running" {
				return nil
			}
			if result.State != "succeeded" && result.State != "failed" {
				return errors.New("evidence_incomplete")
			}
			return s.FinishAttempt(ctx, l, t, a, result.State, result)
		}
		if a.Status != "succeeded" {
			return errors.New("evidence_incomplete")
		}
		if core.Index == nil {
			var uploaded UploadJobResult
			if strictDecode(a.Result, &uploaded) != nil || uploaded.Report == nil {
				return errors.New("evidence_incomplete")
			}
			reader, e := r.ResolveReader(ctx, *config.ArtifactTarget)
			if e != nil {
				return e
			}
			raw, e := reader.Read(ctx, uploaded.Report.Index)
			if e != nil {
				return e
			}
			var index ArtifactIndex
			if strictDecode(raw, &index) != nil || ValidateArtifactIndex(index, core.Manifest.Payload, core.ManifestHash) != nil {
				return errors.New("artifact_conflict")
			}
			key, e := r.ResolveSigner(ctx, *config.SigningKey)
			if e != nil {
				return e
			}
			defer clear(key)
			signature, e := signObject("clawmanager:artifact-index:v1", index, key, config.SigningKey.Version)
			if e != nil {
				return e
			}
			return s.FreezeCoreIndex(ctx, l, t, SignedIndex{Payload: index, Signature: signature}, key.Public().(ed25519.PublicKey))
		}
		_, err = s.Advance(ctx, t, l.Owner, "publishing", "none")
		return err
	}
	if t.Status != "publishing" && t.Status != "finalization_unknown" {
		return errors.New("state_conflict")
	}
	if core.Index == nil {
		return errors.New("evidence_incomplete")
	}
	refs := map[string]ObjectReference{}
	for _, kind := range []string{"manifest_put", "index_put", "marker_put"} {
		action, e := s.GetExternalAction(ctx, t.ID, kind)
		if errors.Is(e, sql.ErrNoRows) {
			var now time.Time
			if e = s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
				return e
			}
			if t.Status == "finalization_unknown" || !now.Before(t.DeadlineAt) {
				return r.failAfterCleanup(ctx, s, l, t, core, stages, "deadline_exceeded")
			}
			var payload any
			key := ""
			switch kind {
			case "manifest_put":
				payload = core.Manifest
				key = "manifest.json"
			case "index_put":
				payload = *core.Index
				key = "index.json"
			case "marker_put":
				commit := CommitPayload{Schema: "system-backup-commit.v1", BackupID: t.ID, OriginInstallationID: s.InstallationID, Manifest: refs["manifest_put"], Index: refs["index_put"]}
				private, e := r.ResolveSigner(ctx, *config.SigningKey)
				if e != nil {
					return e
				}
				signature, e := signObject("clawmanager:artifact-commit:v1", commit, private, config.SigningKey.Version)
				clear(private)
				if e != nil {
					return e
				}
				payload = SignedCommit{Payload: commit, Signature: signature}
				key = "_COMMITTED"
			}
			raw, e := canonicalJSON(payload)
			if e != nil {
				return e
			}
			action, t, e = s.PrepareExternalAction(ctx, l, t, FinalWriteRequest{Type: kind, Target: *config.ArtifactTarget, Key: artifactPrefix(s.InstallationID, t.ID) + key, Payload: raw, Deadline: t.DeadlineAt})
			if e != nil {
				return e
			}
		}
		if e != nil {
			return e
		}
		if action.State == "conflict" {
			return r.failAfterCleanup(ctx, s, l, t, core, stages, "artifact_conflict")
		}
		if action.State == "confirmed_absent" {
			return r.failAfterCleanup(ctx, s, l, t, core, stages, "finalizer_request_unavailable")
		}
		if action.State != "confirmed_present" {
			_, e = r.FinalWrites.Tick(ctx, s, l, t, action)
			return e
		}
		refs[kind] = action.Reference
	}
	if err = s.MarkCoreCommitted(ctx, l, t); err != nil {
		return err
	}
	var now time.Time
	if err = s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return err
	}
	verified, err := s.GetAttempt(ctx, t.ID, "verify")
	if errors.Is(err, sql.ErrNoRows) {
		if !now.Before(t.DeadlineAt) {
			return r.failAfterCleanup(ctx, s, l, t, core, stages, "deadline_exceeded")
		}
		request := r.VerifyTemplate
		request.Origin = s.InstallationID
		request.BackupID = t.ID
		request.MaxBytes = config.MaxArtifactBytes
		request.MaxItems = int(config.MaxSourceItems)
		request.ValidateSnapshot = true
		request.DeadlineAt = t.DeadlineAt
		request.DeadlineSeconds = int64(t.DeadlineAt.Sub(now) / time.Second)
		if request.BootstrapSecret == capture.Request.Ordinary.BootstrapSecret || request.BootstrapSecret == capture.Request.Secrets.BootstrapSecret || request.BootstrapSecret == core.Upload.BootstrapSecret {
			return errors.New("isolation_invalid")
		}
		_, err = s.PrepareAttempt(ctx, l, t, "verify", request)
		return err
	}
	if err != nil {
		return err
	}
	if verified.Status == "failed" || verified.Status == "abandoned" {
		reason := "runner_failed"
		var result VerifyJobResult
		if strictDecode(verified.Result, &result) == nil && result.Failure != "none" && result.Failure != "" && slices.Contains(FailureCategories, result.Failure) {
			reason = result.Failure
		}
		return r.failAfterCleanup(ctx, s, l, t, core, stages, reason)
	}
	if verified.Status == "running" {
		if !now.Before(t.DeadlineAt) {
			return r.failAfterCleanup(ctx, s, l, t, core, stages, "deadline_exceeded")
		}
		binding, e := s.ensureBackupExecution(ctx, l, t, verified,
			func(step context.Context) (*batchv1.Job, error) {
				return r.Verifier.EnsureBound(step, verified, "", true)
			},
			func(step context.Context) (*batchv1.Job, error) { return r.Verifier.Existing(step, verified) },
			func(step context.Context, job *batchv1.Job) error {
				return s.BindBackupVerifyJob(step, l, t, verified, job)
			})
		if e != nil {
			return e
		}
		result, e := r.Verifier.ObserveBound(ctx, verified, binding.UID)
		if e != nil {
			return e
		}
		if result.State == "running" {
			return nil
		}
		if result.State == "succeeded" {
			v := result.Report
			if result.JobUID != binding.UID || v == nil || !v.SnapshotValidated || !v.IntegrityVerified || v.AcceptanceEligible || v.PartsVerified != len(RequiredParts) || v.Origin != s.InstallationID || v.BackupID != t.ID || v.ManifestSHA256 != refs["manifest_put"].SHA256 || v.IndexSHA256 != refs["index_put"].SHA256 || v.CommitSHA256 != refs["marker_put"].SHA256 {
				return s.FinishAttempt(ctx, l, t, verified, "failed", VerifyJobResult{State: "failed", JobUID: binding.UID, Failure: "checksum_mismatch"})
			}
		} else if result.State != "failed" {
			return errors.New("evidence_incomplete")
		}
		return s.FinishAttempt(ctx, l, t, verified, result.State, result)
	}
	if verified.Status != "succeeded" {
		return errors.New("evidence_incomplete")
	}
	done, err := r.stopAllJobs(ctx, s, l, t)
	if err != nil || !done {
		return err
	}
	done, err = stages.Cleanup(ctx, s, l, t, &capture)
	if err != nil || !done {
		return err
	}
	done, err = r.Resources.Cleanup(ctx, s, l, t, core)
	if err != nil || !done {
		return err
	}
	core, err = s.RecordCoreCleanup(ctx, l, t)
	if err != nil {
		return err
	}
	if t.Purpose == "functional_test" {
		return s.CompleteFunctionalBackup(ctx, l, t)
	}
	return r.Acceptance.Tick(ctx, s, l, t, core)
}

func (r CorePublisher) stopAllJobs(ctx context.Context, s Store, l Leadership, t Task) (bool, error) {
	all := true
	for _, phase := range []string{"capture", "secret_capture", "upload", "verify"} {
		a, err := s.GetAttempt(ctx, t.ID, phase)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		binding, err := s.CaptureBinding(ctx, a.ID)
		if errors.Is(err, sql.ErrNoRows) {
			sent, e := s.backupJobWasSent(ctx, a)
			if e != nil {
				return false, e
			}
			if !sent {
				continue
			}
			var job *batchv1.Job
			switch phase {
			case "capture", "secret_capture":
				job, err = r.CaptureJobs.Existing(ctx, a)
			case "upload":
				job, err = r.Uploads.Existing(ctx, a)
			case "verify":
				job, err = r.Verifier.Existing(ctx, a)
			}
			if apierrors.IsNotFound(err) {
				all = false
				continue
			}
			if err != nil {
				return false, err
			}
			switch phase {
			case "capture", "secret_capture":
				err = s.BindCaptureJob(ctx, l, t, a, job)
			case "upload":
				err = s.BindUploadJob(ctx, l, t, a, job)
			case "verify":
				err = s.BindBackupVerifyJob(ctx, l, t, a, job)
			}
			if err != nil {
				return false, err
			}
			binding, err = s.CaptureBinding(ctx, a.ID)
		}
		if err != nil {
			return false, err
		}
		var done bool
		switch phase {
		case "capture", "secret_capture":
			done, err = r.CaptureJobs.Stop(ctx, a, binding.UID)
		case "upload":
			done, err = r.Uploads.Stop(ctx, a, binding.UID)
		case "verify":
			done, err = r.Verifier.StopBound(ctx, a, binding.UID)
		}
		if err != nil {
			return false, err
		}
		all = all && done
		if done {
			if err = s.MarkBackupJobStopped(ctx, l, t, a, binding.UID); err != nil {
				return false, err
			}
		}
	}
	return all, nil
}
func (r CorePublisher) Abort(ctx context.Context, s Store, l Leadership, t Task) (bool, error) {
	done, err := r.stopAllJobs(ctx, s, l, t)
	if err != nil || !done {
		return false, err
	}
	core, err := s.CorePublication(ctx, t.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return r.Resources.Cleanup(ctx, s, l, t, core)
}
func (r CorePublisher) failAfterCleanup(ctx context.Context, s Store, l Leadership, t Task, core CorePublication, stages CaptureResourceDriver, reason string) error {
	done, err := r.stopAllJobs(ctx, s, l, t)
	if err != nil || !done {
		return err
	}
	capture, err := s.GetCapturePlan(ctx, t.ID)
	if err != nil {
		return err
	}
	done, err = stages.Cleanup(ctx, s, l, t, &capture)
	if err != nil || !done {
		return err
	}
	done, err = r.Resources.Cleanup(ctx, s, l, t, core)
	if err != nil || !done {
		return err
	}
	_, err = s.Advance(ctx, t, l.Owner, "failed", reason)
	return err
}
