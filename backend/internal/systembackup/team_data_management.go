package systembackup

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

type TeamDataDeleteConfirmation struct {
	Origin          string    `json:"origin"`
	BackupID        string    `json:"backup_id"`
	Actor           int       `json:"actor"`
	InventorySHA256 string    `json:"inventory_sha256"`
	CommitSHA256    string    `json:"commit_sha256"`
	ExpiresAt       time.Time `json:"expires_at"`
	Nonce           string    `json:"nonce"`
}
type signedTeamDataDeleteConfirmation struct {
	Payload   TeamDataDeleteConfirmation `json:"payload"`
	Signature ArtifactSignature          `json:"signature"`
}
type TeamDataDeletePreview struct {
	BackupID     string    `json:"backup_id"`
	Bytes        int64     `json:"bytes"`
	Versions     int       `json:"versions"`
	ExpiresAt    time.Time `json:"expires_at"`
	Confirmation string    `json:"confirmation"`
}

func (c TeamDataCatalogStore) SetProtected(ctx context.Context, id string, actor int, protect bool) error {
	return c.Update(ctx, func(s *TeamDataCatalog) error {
		a, ok := s.Artifacts[id]
		if !ok {
			return errors.New("not_found")
		}
		if a.Deleted || a.Deleting {
			return errors.New("backup_deleting")
		}
		a.Protected = protect
		s.Artifacts[id] = a
		action := "unprotect"
		if protect {
			action = "protect"
		}
		s.Audit = append(s.Audit, TeamDataAudit{time.Now().UTC(), actor, action, id})
		return nil
	})
}
func teamDataDeletionGuard(state TeamDataCatalog, id string, minimum int) error {
	a, ok := state.Artifacts[id]
	if !ok || a.Deleted {
		return errors.New("not_found")
	}
	if a.Deleting {
		return errors.New("backup_deleting")
	}
	if a.Protected {
		return errors.New("backup_protected")
	}
	if len(a.Leases) > 0 {
		return errors.New("backup_in_use")
	}
	if minimum < 1 {
		minimum = 1
	}
	remaining := 0
	for other, v := range state.Artifacts {
		if other != id && !v.Deleted && !v.Deleting && v.Health == "verified" && v.VerifiedAt != nil {
			remaining++
		}
	}
	if remaining < minimum {
		return errors.New("minimum_recovery_points")
	}
	return nil
}
func (c TeamDataCatalogStore) artifactVersions(ctx context.Context, id string) (PublishedArtifact, []ObjectReference, error) {
	a, e := OpenPublishedArtifact(ctx, c.Store, c.Origin, id, c.PublicKey, c.KeyVersion)
	if e != nil {
		return a, nil, e
	}
	if a.Manifest.Payload.Purpose != "team_data" {
		return a, nil, errors.New("artifact_conflict")
	}
	refs := []ObjectReference{}
	for _, p := range a.Index.Payload.Parts {
		refs = append(refs, p.Objects...)
	}
	// Publication metadata goes last so a partial deletion retains the signed
	// identity as long as possible. The durable tombstone survives separately.
	refs = append(refs, a.Commit.Payload.Index, a.Commit.Payload.Manifest, a.CommitReference)
	allowed := map[string]bool{}
	for _, r := range refs {
		allowed[r.Key+"\x00"+r.VersionID] = true
	}
	prefix, e := c.Store.key(artifactPrefix(c.Origin, id) + "placeholder")
	if e != nil {
		return a, nil, e
	}
	prefix = strings.TrimSuffix(prefix, "placeholder")
	seen := 0
	for object := range c.Store.Client.ListObjects(ctx, c.Store.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			return a, nil, errors.New("artifact_unavailable")
		}
		seen++
		if seen > len(refs) || !allowed[strings.TrimPrefix(object.Key, c.Store.Root+"/")+"\x00"+object.VersionID] {
			return a, nil, errors.New("artifact_conflict")
		}
	}
	if e := ctx.Err(); e != nil {
		return a, nil, e
	}
	return a, refs, nil
}
func (c TeamDataCatalogStore) PreviewDelete(ctx context.Context, id string, actor, minimum int) (TeamDataDeletePreview, error) {
	var result TeamDataDeletePreview
	state, _, e := c.Load(ctx)
	if e != nil {
		return result, e
	}
	if e = teamDataDeletionGuard(state, id, minimum); e != nil {
		return result, e
	}
	a, refs, e := c.artifactVersions(ctx, id)
	if e != nil {
		return result, e
	}
	if state.Artifacts[id].CommitSHA256 != a.CommitReference.SHA256 {
		return result, errors.New("artifact_conflict")
	}
	hash, e := RequestHash(refs)
	if e != nil {
		return result, e
	}
	nonce, e := RandomID("confirm_")
	if e != nil {
		return result, e
	}
	payload := TeamDataDeleteConfirmation{c.Origin, id, actor, hash, a.CommitReference.SHA256, time.Now().UTC().Add(5 * time.Minute), nonce}
	sig, e := signObject("clawmanager:team-data-delete-confirmation:v1", payload, c.Signer, c.KeyVersion)
	if e != nil {
		return result, e
	}
	raw, e := canonicalJSON(signedTeamDataDeleteConfirmation{payload, sig})
	if e != nil {
		return result, e
	}
	return TeamDataDeletePreview{id, state.Artifacts[id].Bytes, len(refs), payload.ExpiresAt, base64.RawURLEncoding.EncodeToString(raw)}, nil
}
func (c TeamDataCatalogStore) BeginDelete(ctx context.Context, id, token string, actor, minimum int) (string, error) {
	if len(token) > 4096 {
		return "", errors.New("confirmation_invalid")
	}
	raw, e := base64.RawURLEncoding.Strict().DecodeString(token)
	var signed signedTeamDataDeleteConfirmation
	if e != nil || strictDecode(raw, &signed) != nil || verifyObject("clawmanager:team-data-delete-confirmation:v1", signed.Payload, signed.Signature, c.PublicKey, c.KeyVersion) != nil {
		return "", errors.New("confirmation_invalid")
	}
	p := signed.Payload
	if actor < 1 || p.Actor != actor || p.BackupID != id || p.Origin != c.Origin || !p.ExpiresAt.After(time.Now()) || p.ExpiresAt.After(time.Now().Add(6*time.Minute)) {
		return "", errors.New("confirmation_invalid")
	}
	a, refs, e := c.artifactVersions(ctx, p.BackupID)
	if e != nil {
		return "", e
	}
	hash, e := RequestHash(refs)
	if e != nil {
		return "", e
	}
	if hash != p.InventorySHA256 || a.CommitReference.SHA256 != p.CommitSHA256 {
		return "", errors.New("confirmation_invalid")
	}
	e = c.Update(ctx, func(s *TeamDataCatalog) error {
		if e := teamDataDeletionGuard(*s, p.BackupID, minimum); e != nil {
			return e
		}
		v := s.Artifacts[p.BackupID]
		if v.CommitSHA256 != p.CommitSHA256 {
			return errors.New("artifact_conflict")
		}
		v.Deleting = true
		v.DeleteActor = actor
		v.DeleteVersions = refs
		v.DeletedVersions = 0
		s.Artifacts[p.BackupID] = v
		s.Audit = append(s.Audit, TeamDataAudit{time.Now().UTC(), actor, "delete_confirmed", p.BackupID})
		return nil
	})
	return p.BackupID, e
}
func (c TeamDataCatalogStore) ContinueDelete(ctx context.Context, id string, actor int, progress func(int) error) error {
	state, _, e := c.Load(ctx)
	if e != nil {
		return e
	}
	record, ok := state.Artifacts[id]
	if !ok || record.DeleteActor != actor || !record.Deleting || record.Deleted || len(record.Leases) > 0 {
		return errors.New("state_conflict")
	}
	for i := record.DeletedVersions; i < len(record.DeleteVersions); i++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		ref := record.DeleteVersions[i]
		if !strings.HasPrefix(ref.Key, artifactPrefix(c.Origin, id)) {
			return errors.New("artifact_conflict")
		}
		key, e := c.Store.key(ref.Key)
		if e != nil {
			return e
		}
		if e = c.Store.Client.RemoveObject(ctx, c.Store.Bucket, key, minio.RemoveObjectOptions{VersionID: ref.VersionID}); e != nil {
			return errors.New("artifact_delete_failed")
		}
		_, e = c.Store.Client.StatObject(ctx, c.Store.Bucket, key, minio.StatObjectOptions{VersionID: ref.VersionID})
		code := minio.ToErrorResponse(e).Code
		if e == nil || code != "NoSuchKey" && code != "NoSuchVersion" && code != "NoSuchObject" {
			return errors.New("artifact_delete_failed")
		}
		e = c.Update(ctx, func(s *TeamDataCatalog) error {
			v := s.Artifacts[id]
			if !v.Deleting || v.DeleteActor != actor || len(v.Leases) > 0 {
				return errors.New("state_conflict")
			}
			if v.DeletedVersions < i+1 {
				v.DeletedVersions = i + 1
			}
			s.Artifacts[id] = v
			return nil
		})
		if e != nil {
			return e
		}
		if progress != nil {
			if e = progress(i + 1); e != nil {
				return e
			}
		}
	}
	prefix, _ := c.Store.key(artifactPrefix(c.Origin, id) + "placeholder")
	prefix = strings.TrimSuffix(prefix, "placeholder")
	for item := range c.Store.Client.ListObjects(ctx, c.Store.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if item.Err != nil {
			return errors.New("artifact_unavailable")
		}
		return errors.New("artifact_conflict")
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return c.Update(ctx, func(s *TeamDataCatalog) error {
		v := s.Artifacts[id]
		if !v.Deleting || v.DeleteActor != actor || len(v.Leases) > 0 {
			return errors.New("state_conflict")
		}
		v.Deleted = true
		v.Deleting = false
		v.Health = "deleted"
		s.Artifacts[id] = v
		s.Audit = append(s.Audit, TeamDataAudit{time.Now().UTC(), actor, "deleted", id})
		return nil
	})
}
func (s *TeamDataService) minimumRecoveryPoints() int {
	if s.cfg.MinimumRecoveryPoints < 1 {
		return 1
	}
	return s.cfg.MinimumRecoveryPoints
}
func (s *TeamDataService) Protect(ctx context.Context, id string, actor int, protect bool) error {
	if s.cfg.Disabled && !protect {
		return errors.New("feature_disabled")
	}
	return s.catalog().SetProtected(ctx, id, actor, protect)
}
func (s *TeamDataService) DeletePreview(ctx context.Context, id string, actor int) (TeamDataDeletePreview, error) {
	if s.cfg.Disabled {
		return TeamDataDeletePreview{}, errors.New("feature_disabled")
	}
	return s.catalog().PreviewDelete(ctx, id, actor, s.minimumRecoveryPoints())
}
func (s *TeamDataService) DeleteConfirmed(ctx context.Context, id, token, key string, actor int) (TeamDataJob, error) {
	if s.cfg.Disabled {
		return TeamDataJob{}, errors.New("feature_disabled")
	}
	if actor < 1 || !idemPattern.MatchString(key) {
		return TeamDataJob{}, errors.New("validation_failed")
	}
	if s.db != nil {
		previous, e := scanTeamDataJob(s.db.QueryRowContext(ctx, "SELECT "+teamDataColumns+" FROM system_team_data_backup_jobs WHERE actor_id=? AND idempotency_key=?", actor, key))
		if e == nil {
			if previous.Operation != "delete" || previous.SourceID != id {
				return TeamDataJob{}, errors.New("idempotency_conflict")
			}
			return previous, nil
		}
		if e.Error() != "not_found" {
			return TeamDataJob{}, e
		}
		var active int
		if e = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_team_data_backup_jobs WHERE status IN ('pending','running','canceling')").Scan(&active); e != nil {
			return TeamDataJob{}, e
		}
		if active > 0 {
			return TeamDataJob{}, errors.New("conflict_active_job")
		}
	}
	confirmed, e := s.catalog().BeginDelete(ctx, id, token, actor, s.minimumRecoveryPoints())
	if e != nil {
		return TeamDataJob{}, e
	}
	if confirmed != id {
		return TeamDataJob{}, errors.New("confirmation_invalid")
	}
	return s.Create(ctx, actor, key, "delete", id)
}
func (s *TeamDataService) verifyBackup(ctx context.Context, j TeamDataJob) (r TeamDataReport, err error) {
	r = TeamDataReport{BackupID: j.SourceID, Purpose: "team_data", Consistency: "best_effort", SourceUnaffected: true}
	// A queued job already owns a reference. Record failures and release it
	// even when opening commit/manifest fails before content validation starts.
	defer func() {
		c, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		operationErr := err
		updateErr := s.catalog().Update(c, func(state *TeamDataCatalog) error {
			v, ok := state.Artifacts[j.SourceID]
			if !ok || v.Deleted || v.Deleting {
				return nil
			}
			now := time.Now().UTC()
			v.CheckedAt = &now
			v.Health = "verified"
			v.LastError = ""
			if operationErr == nil {
				v.VerifiedAt = &now
			} else {
				v.Health = "unavailable"
				code := operationErr.Error()
				if strings.Contains(code, "checksum") || code == "artifact_conflict" || code == "signature_invalid" {
					v.Health = "damaged"
				}
				if strings.Contains(code, "key") || strings.Contains(code, "authentication") {
					v.Health = "key_unavailable"
				}
				v.LastError = PublicError(operationErr)
			}
			state.Artifacts[j.SourceID] = v
			return nil
		})
		releaseErr := s.catalog().Release(c, j.SourceID, j.ID, "job:"+j.ID)
		if err == nil {
			err = errors.Join(updateErr, releaseErr)
		}
	}()
	a, e := OpenPublishedArtifact(ctx, s.store, s.cfg.InstallationID, j.SourceID, s.catalog().PublicKey, s.cfg.KeyVersion)
	if e != nil {
		return r, e
	}
	if e = s.catalog().Import(ctx, a, 0, false); e != nil {
		return r, e
	}
	if e = s.catalog().Acquire(ctx, j.SourceID, j.ID, "job:"+j.ID); e != nil {
		return r, e
	}
	if e = s.phase(ctx, j, "verifying_backup"); e != nil {
		return r, e
	}
	e = ValidatePublishedSnapshot(ctx, s.store, a, s.wrapper, CaptureBudget{s.cfg.MaxBytes, s.cfg.MaxItems})
	if e != nil {
		return r, e
	}
	r.IntegrityVerified = true
	for _, p := range a.Manifest.Payload.Parts {
		r.Parts = append(r.Parts, TeamDataPart{p.Name, p.CiphertextBytes, p.CiphertextSHA256})
	}
	return r, nil
}
