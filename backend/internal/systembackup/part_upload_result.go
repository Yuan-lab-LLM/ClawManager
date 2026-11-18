package systembackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

type PartUploadRequest struct {
	Origin           string `json:"origin"`
	BackupID         string `json:"backup_id"`
	AttemptID        string `json:"attempt_id"`
	OrdinaryStageUID string `json:"ordinary_stage_uid"`
	SecretStageUID   string `json:"secret_stage_uid"`
}
type PartUploadReport struct {
	Schema string `json:"schema"`
	PartUploadRequest
	Index ObjectReference `json:"index"`
	Parts int             `json:"parts"`
}

func (r PartUploadRequest) Validate() error {
	if !validPartIdentity(r.Origin, r.BackupID, "mysql") || !attemptIDPattern.MatchString(r.AttemptID) || !refPattern.MatchString(r.OrdinaryStageUID) || !refPattern.MatchString(r.SecretStageUID) || r.OrdinaryStageUID == r.SecretStageUID {
		return errors.New("validation_failed")
	}
	return nil
}
func (r PartUploadRequest) indexKey() string {
	return artifactPrefix(r.Origin, r.BackupID) + "candidates/" + r.AttemptID + ".json"
}
func SealUploadedIndex(ctx context.Context, store PartUploadStore, r PartUploadRequest, index ArtifactIndex) (PartUploadReport, error) {
	var zero PartUploadReport
	if store == nil || r.Validate() != nil || index.BackupID != r.BackupID || index.OriginInstallationID != r.Origin || len(index.Parts) != len(RequiredParts) {
		return zero, errors.New("validation_failed")
	}
	raw, err := canonicalJSON(index)
	if err != nil {
		return zero, err
	}
	if int64(len(raw)) > MaxImmutableObjectBytes {
		return zero, errors.New("capacity_exceeded")
	}
	ref, err := store.PutImmutable(ctx, r.indexKey(), raw)
	if err != nil {
		return zero, err
	}
	h := sha256.Sum256(raw)
	if ref.Key != r.indexKey() || ref.VersionID == "" || ref.VersionID == "null" || ref.Bytes != int64(len(raw)) || ref.SHA256 != hex.EncodeToString(h[:]) {
		return zero, errors.New("evidence_incomplete")
	}
	return PartUploadReport{Schema: "system-backup-upload.v1", PartUploadRequest: r, Index: ref, Parts: len(index.Parts)}, nil
}
