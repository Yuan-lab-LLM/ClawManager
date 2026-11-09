package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// LocalCaptureResourceConfig is offline installation configuration. Only the
// single-node provider uses HostPath; a cluster profile needs its own proven
// snapshot/checkpoint provider and cannot silently fall back to this one.
type LocalCaptureResourceConfig struct {
	Capture        CaptureJobRequest `json:"capture"`
	Secrets        CaptureJobRequest `json:"secret_capture"`
	MountDirectory string            `json:"mount_directory"`
	HostRoot       string            `json:"host_root"`
	NodeName       string            `json:"node_name"`
	StorageClass   string            `json:"storage_class"`
}
type LocalCaptureResources struct {
	Volumes LocalStageVolumes
	Config  LocalCaptureResourceConfig
}

func (r LocalCaptureResources) Prepare(ctx context.Context, s Store, l Leadership, t Task, c BackupConfig) (CapturePlanRequest, error) {
	var zero CapturePlanRequest
	if c.Profile != "single-node" {
		return zero, errors.New("unsupported_profile_p1")
	}
	if r.Volumes.MountDirectory != r.Config.MountDirectory || r.Config.Capture.Namespace != r.Config.Secrets.Namespace || r.Config.Capture.NamespaceUID != r.Config.Secrets.NamespaceUID || r.Config.Capture.BootstrapSecret == r.Config.Secrets.BootstrapSecret || r.Config.Capture.OperationSecret == r.Config.Secrets.OperationSecret {
		return zero, errors.New("isolation_invalid")
	}
	var writerHash string
	if err := s.DB.QueryRowContext(ctx, "SELECT evidence_hash FROM system_backup_dependency_health WHERE config_version=? AND dependency='writer_registry' AND status='healthy' AND expires_at>UTC_TIMESTAMP(6)", t.ConfigVersion).Scan(&writerHash); errors.Is(err, sql.ErrNoRows) {
		return zero, errors.New("not_ready")
	} else if err != nil {
		return zero, err
	}
	gate, err := s.captureGate(ctx, r.Config.Capture.Gate.Scope)
	if err != nil {
		return zero, err
	}
	if gate.Phase != "idle" {
		return zero, ErrMaintenance
	}
	own := GateOwnership{Scope: r.Config.Capture.Gate.Scope, Owner: t.ID, Generation: gate.Generation + 1, Token: gate.Token + 1}
	var now time.Time
	if err = s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return zero, err
	}
	seconds := int64(t.DeadlineAt.Sub(now) / time.Second)
	if seconds < 1 {
		return zero, errors.New("deadline_exceeded")
	}
	ordinary, secrets := r.Config.Capture, r.Config.Secrets
	if c.MaxArtifactBytes < 2<<20 {
		return zero, errors.New("capacity_exceeded")
	}
	secretBudget := max(1<<20, min(c.MaxArtifactBytes/4, 256<<20))
	ordinary.MaxBytes = c.MaxArtifactBytes - secretBudget
	secrets.MaxBytes = secretBudget
	for _, job := range []*CaptureJobRequest{&ordinary, &secrets} {
		job.Origin = s.InstallationID
		job.BackupID = t.ID
		job.Gate = own
		job.DeadlineAt = t.DeadlineAt
		job.DeadlineSeconds = seconds
		job.MaxItems = int(c.MaxSourceItems)
		job.NodeName = r.Config.NodeName
	}
	ordinary.Phase = "capture"
	secrets.Phase = "secret_capture"
	secrets.SourceVolumes = nil
	ready := true
	for i, job := range []*CaptureJobRequest{&ordinary, &secrets} {
		capacity := c.StagingCapacityBytes - c.StagingCapacityBytes/4
		if i == 1 {
			capacity = c.StagingCapacityBytes / 4
		}
		request := StageVolumeRequest{Phase: job.Phase, Namespace: job.Namespace, NamespaceUID: job.NamespaceUID, NodeName: r.Config.NodeName, HostRoot: r.Config.HostRoot, StorageClass: r.Config.StorageClass, CapacityBytes: capacity}
		v, e := s.PrepareStageVolume(ctx, l, t, request)
		if e != nil {
			return zero, e
		}
		v, bound, e := r.Volumes.Ensure(ctx, s, l, t, v)
		if e != nil {
			return zero, e
		}
		ready = ready && bound
		job.Stage = PVCIdentity{Name: v.Name(), UID: v.PVCUID}
	}
	if !ready {
		return zero, ErrMaintenance
	}
	return CapturePlanRequest{Ordinary: ordinary, Secrets: secrets, WriterRegistryHash: writerHash}, nil
}
func (r LocalCaptureResources) Cleanup(ctx context.Context, s Store, l Leadership, t Task, p *CapturePlan) (bool, error) {
	all := true
	for _, phase := range []string{"capture", "secret_capture"} {
		v, err := s.StageVolume(ctx, t.ID, phase)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		done, err := r.Volumes.Cleanup(ctx, s, l, t, v)
		if err != nil {
			return false, err
		}
		all = all && done
	}
	return all, nil
}
