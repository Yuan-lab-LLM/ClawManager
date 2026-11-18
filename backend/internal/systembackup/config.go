package systembackup

import (
	"errors"
	"regexp"
)

// References are immutable provider identities, never key material, passwords,
// URLs with credentials, arbitrary local paths, or mutable 'latest' aliases.
type SecretRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Purpose  string `json:"purpose"`
}

var refPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func (r SecretRef) Validate(purpose string) error {
	if r.Purpose != purpose || !refPattern.MatchString(r.Name) || !refPattern.MatchString(r.Version) || r.Version == "latest" || (r.Provider != "kubernetes" && r.Provider != "vault") {
		return errors.New("validation_failed")
	}
	return nil
}

type BackupConfig struct {
	Enabled                bool       `json:"enabled"`
	Runner                 string     `json:"runner"`
	Profile                string     `json:"profile"`
	RetentionSeconds       int64      `json:"retention_seconds"`
	MinimumRecoveryPoints  int        `json:"minimum_recovery_points"`
	GateAcquisitionSeconds int        `json:"gate_acquisition_timeout_seconds"`
	MaxHoldSeconds         int        `json:"quiesce_max_hold_seconds"`
	TaskDeadlineSeconds    int        `json:"task_deadline_seconds"`
	MaxArtifactBytes       int64      `json:"max_artifact_bytes"`
	StagingCapacityBytes   int64      `json:"staging_capacity_bytes"`
	MaxIndexBytes          int64      `json:"max_index_bytes"`
	MaxSourceItems         int64      `json:"max_source_items"`
	ArtifactTarget         *SecretRef `json:"artifact_target,omitempty"`
	EncryptionKey          *SecretRef `json:"encryption_key,omitempty"`
	SigningKey             *SecretRef `json:"signing_key,omitempty"`
	Catalog                *SecretRef `json:"catalog,omitempty"`
}

func DefaultConfig() BackupConfig {
	return BackupConfig{Runner: "k8s", Profile: "single-node", RetentionSeconds: 2592000, MinimumRecoveryPoints: 2, GateAcquisitionSeconds: 60, MaxHoldSeconds: 600, TaskDeadlineSeconds: 21600, MaxArtifactBytes: 17179869184, StagingCapacityBytes: 21474836480, MaxIndexBytes: 536870912, MaxSourceItems: 5000000}
}

func (c BackupConfig) Validate() error {
	if c.Runner != "k8s" {
		return errors.New("unsupported_runner_p1")
	}
	if c.Profile != "cluster" && c.Profile != "single-node" {
		return errors.New("unsupported_profile_p1")
	}
	if c.RetentionSeconds < 86400 || c.RetentionSeconds > 31536000 || c.MinimumRecoveryPoints < 1 || c.MinimumRecoveryPoints > 100 || c.GateAcquisitionSeconds < 10 || c.GateAcquisitionSeconds > 600 || c.MaxHoldSeconds < 30 || c.MaxHoldSeconds > 3600 || c.TaskDeadlineSeconds < 600 || c.TaskDeadlineSeconds > 43200 || c.MaxArtifactBytes < 1<<20 || c.MaxArtifactBytes > 900<<30 || c.MaxIndexBytes < 1<<20 || c.MaxIndexBytes > 8<<30 || c.StagingCapacityBytes < 1<<30 || c.StagingCapacityBytes > 1<<40 || c.MaxSourceItems < 1 || c.MaxSourceItems > 100000000 {
		return errors.New("validation_failed")
	}
	if c.StagingCapacityBytes < (c.MaxArtifactBytes*110+99)/100+c.MaxIndexBytes {
		return errors.New("capacity_exceeded")
	}
	for _, r := range []struct {
		ref     *SecretRef
		purpose string
	}{{c.ArtifactTarget, "artifact"}, {c.EncryptionKey, "encryption"}, {c.SigningKey, "signing"}, {c.Catalog, "catalog"}} {
		if r.ref != nil {
			if e := r.ref.Validate(r.purpose); e != nil {
				return e
			}
		} else if c.Enabled {
			return errors.New("not_ready")
		}
	}
	return nil
}

type ConfigVersion struct {
	Version uint64       `json:"version"`
	Config  BackupConfig `json:"config"`
}

func SecurityConfigChanged(a, b BackupConfig) bool {
	for _, pair := range [][2]*SecretRef{{a.ArtifactTarget, b.ArtifactTarget}, {a.EncryptionKey, b.EncryptionKey}, {a.SigningKey, b.SigningKey}, {a.Catalog, b.Catalog}} {
		if (pair[0] == nil) != (pair[1] == nil) {
			return true
		}
		if pair[0] != nil && *pair[0] != *pair[1] {
			return true
		}
	}
	return a.Runner != b.Runner || a.Profile != b.Profile
}
