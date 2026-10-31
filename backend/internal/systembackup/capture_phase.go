package systembackup

import (
	"context"
	"errors"
	"os"
	"time"
)

type CaptureGuardSnapshot struct {
	Ownership GateOwnership
	Now       time.Time
}
type CaptureGuard interface {
	Snapshot(context.Context) (CaptureGuardSnapshot, error)
}
type SQLCaptureGuard struct {
	Gate        Gate
	Ownership   GateOwnership
	AbortMargin time.Duration
}

func (g SQLCaptureGuard) Snapshot(ctx context.Context) (CaptureGuardSnapshot, error) {
	s := CaptureGuardSnapshot{Ownership: g.Ownership}
	var valid bool
	if g.Gate.DB == nil || g.AbortMargin < 0 || g.AbortMargin > time.Minute {
		return s, errors.New("validation_failed")
	}
	err := g.Gate.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6),phase='held' AND owner=? AND generation=? AND fencing_token=? AND UTC_TIMESTAMP(6)<TIMESTAMPADD(MICROSECOND,?,hold_deadline) AND NOT EXISTS (SELECT 1 FROM system_backup_participants p WHERE p.scope=g.scope AND (p.generation<>g.generation OR p.state<>'paused' OR p.resume_result<>'none')) FROM system_backup_gate g WHERE scope=?", g.Ownership.Owner, g.Ownership.Generation, g.Ownership.Token, -g.AbortMargin.Microseconds(), g.Ownership.Scope).Scan(&s.Now, &valid)
	if err != nil {
		return s, err
	}
	if !valid {
		return s, ErrFence
	}
	return s, nil
}

type CapturePhaseRequest struct {
	Origin    string        `json:"origin_installation_id"`
	BackupID  string        `json:"backup_id"`
	AttemptID string        `json:"attempt_id"`
	Phase     string        `json:"phase"`
	Gate      GateOwnership `json:"gate"`
	StageUID  string        `json:"staging_uid"`
}
type CaptureCandidate struct {
	Schema string `json:"schema"`
	CapturePhaseRequest
	RegistryHash string         `json:"registry_hash"`
	StartedAt    time.Time      `json:"started_at"`
	SealedAt     time.Time      `json:"sealed_at"`
	Parts        []ArtifactPart `json:"parts"`
}

func (r CapturePhaseRequest) Validate() error {
	if !validPartIdentity(r.Origin, r.BackupID, "mysql") || !attemptIDPattern.MatchString(r.AttemptID) || !refPattern.MatchString(r.StageUID) || r.Gate.Owner != r.BackupID || !refPattern.MatchString(r.Gate.Scope) || r.Gate.Generation == 0 || r.Gate.Token == 0 || (r.Phase != "capture" && r.Phase != "secret_capture") {
		return errors.New("validation_failed")
	}
	return nil
}

func (source CaptureSources) ValidateForPhase(phase string) error {
	if phase == "capture" {
		if source.MySQL == nil || source.Redis == nil || source.Objects == nil || source.DeploymentClient == nil || source.SecretClient != nil || len(source.Secrets) != 0 || len(source.ObjectBuckets) == 0 || len(source.Workspace) == 0 {
			return errors.New("capture capabilities are incomplete or combined with secret capture")
		}
		for _, root := range source.Workspace {
			if !refPattern.MatchString(root.ID) || root.Path != "/run/backup-source/"+root.ID {
				return errors.New("capture roots must be the frozen read-only source mounts")
			}
		}
	} else if phase == "secret_capture" {
		if source.SecretClient == nil || source.MySQL != nil || source.Redis != nil || source.Objects != nil || source.DeploymentClient != nil || len(source.Workspace) != 0 || len(source.ObjectBuckets) != 0 || len(source.Deployments) != 0 {
			return errors.New("secret capture requires a separate capability set")
		}
	} else {
		return errors.New("validation_failed")
	}
	return nil
}

// CapturePhase reads either five ordinary sources OR the Secret source, with
// distinct credentials and staging. It has no signing key or final-write
// provider. No candidate is sealed after losing the live gate fence.
func CapturePhase(parent context.Context, stage string, r CapturePhaseRequest, source CaptureSources, b CaptureBudget, wrapper DataKeyWrapper, guard CaptureGuard) (CaptureCandidate, error) {
	var zero CaptureCandidate
	if r.Validate() != nil || b.MaxBytes < 1<<20 || b.MaxItems < 1 || wrapper == nil || guard == nil {
		return zero, errors.New("validation_failed")
	}
	if err := source.ValidateForPhase(r.Phase); err != nil {
		return zero, err
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 0 {
		return zero, errors.New("fresh encrypted staging required")
	}
	first, err := guard.Snapshot(parent)
	if err != nil {
		return zero, err
	}
	if first.Ownership != r.Gate || first.Now.IsZero() {
		return zero, ErrFence
	}
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s, err := guard.Snapshot(ctx)
				if err != nil {
					cancel(err)
					return
				}
				if s.Ownership != r.Gate {
					cancel(ErrFence)
					return
				}
			}
		}
	}()
	defer func() { cancel(context.Canceled); <-done }()
	var tables []string
	if r.Phase == "capture" {
		if err = ValidateSchema(ctx, source.MySQL); err != nil {
			return zero, err
		}
		tables, err = SnapshotBusinessTables(ctx, source.MySQL)
		if err != nil {
			return zero, err
		}
	}
	candidate := CaptureCandidate{Schema: "system-backup-capture-candidate.v1", CapturePhaseRequest: r, RegistryHash: RegistryHash(), StartedAt: first.Now, Parts: []ArtifactPart{}}
	remaining := b.MaxBytes
	for _, adapter := range captureAdapters(source, b, tables) {
		if (adapter.name == "secret_bundle") != (r.Phase == "secret_capture") {
			continue
		}
		if remaining < 1 {
			return zero, errors.New("capacity_exceeded")
		}
		if check, err := guard.Snapshot(ctx); err != nil {
			return zero, err
		} else if check.Ownership != r.Gate {
			return zero, ErrFence
		}
		part, err := CapturePart(ctx, stage, r.Origin, r.BackupID, adapter.name, remaining, wrapper, adapter.capture)
		if err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return zero, cause
			}
			return zero, err
		}
		remaining -= part.CiphertextBytes
		candidate.Parts = append(candidate.Parts, part)
	}
	// Part data is already fsynced. Recheck after the last live-source read,
	// then durably seal metadata before announcing completion to the controller.
	last, err := guard.Snapshot(ctx)
	if err != nil {
		return zero, err
	}
	if last.Ownership != r.Gate {
		return zero, ErrFence
	}
	candidate.SealedAt = last.Now
	if err = context.Cause(ctx); err != nil {
		return zero, err
	}
	raw, err := canonicalJSON(candidate)
	if err != nil {
		return zero, err
	}
	root, err := os.OpenRoot(stage)
	if err != nil {
		return zero, err
	}
	defer root.Close()
	f, err := root.OpenFile("capture-candidate.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return zero, err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = syncDirectory(root)
	}
	if err != nil {
		return zero, err
	}
	if confirmed, checkErr := guard.Snapshot(ctx); checkErr != nil {
		return zero, checkErr
	} else if confirmed.Ownership != r.Gate {
		return zero, ErrFence
	}
	if err = context.Cause(ctx); err != nil {
		return zero, err
	}
	return candidate, nil
}

// Only the controller combines both independently sealed phases. The caller
// must additionally validate writer/provider/verifier/cleanup evidence before
// any acceptance signature or external registration can be issued.
func AggregateCapturedPhases(ctx context.Context, ordinary, secrets CaptureCandidate, b CaptureBudget, guard CaptureGuard) (Manifest, error) {
	var zero Manifest
	if guard == nil || ordinary.CapturePhaseRequest.Validate() != nil || secrets.CapturePhaseRequest.Validate() != nil || ordinary.Schema != "system-backup-capture-candidate.v1" || secrets.Schema != ordinary.Schema || ordinary.Phase != "capture" || secrets.Phase != "secret_capture" || ordinary.BackupID != secrets.BackupID || ordinary.Origin != secrets.Origin || ordinary.Gate != secrets.Gate || ordinary.AttemptID == secrets.AttemptID || ordinary.StageUID == secrets.StageUID || ordinary.RegistryHash != RegistryHash() || secrets.RegistryHash != RegistryHash() || len(ordinary.Parts) != 5 || len(secrets.Parts) != 1 || secrets.Parts[0].Name != "secret_bundle" {
		return zero, errors.New("evidence_incomplete")
	}
	snapshot, err := guard.Snapshot(ctx)
	if err != nil {
		return zero, err
	}
	if snapshot.Ownership != ordinary.Gate {
		return zero, ErrFence
	}
	for _, c := range []CaptureCandidate{ordinary, secrets} {
		if c.StartedAt.IsZero() || c.SealedAt.Before(c.StartedAt) || c.SealedAt.After(snapshot.Now) {
			return zero, errors.New("evidence_incomplete")
		}
	}
	parts := append(append([]ArtifactPart(nil), ordinary.Parts...), secrets.Parts...)
	var total int64
	for _, p := range parts {
		if p.CiphertextBytes > b.MaxBytes-total {
			return zero, errors.New("capacity_exceeded")
		}
		total += p.CiphertextBytes
	}
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: ordinary.BackupID, OriginInstallationID: ordinary.Origin, Purpose: "acceptance", RegistryHash: RegistryHash(), CapturedAt: ordinary.SealedAt, Consistency: "strong", Parts: parts}
	if secrets.SealedAt.After(m.CapturedAt) {
		m.CapturedAt = secrets.SealedAt
	}
	return m, m.Validate()
}
