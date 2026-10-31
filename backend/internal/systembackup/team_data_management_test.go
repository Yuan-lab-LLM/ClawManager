package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/minio/minio-go/v7"
	"strings"
	"sync"
	"testing"
	"time"
)

func teamDataManagementFixture(t *testing.T) (*TeamDataService, PublishedArtifact, PublishedArtifact) {
	t.Helper()
	store := testObjectStore(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	s := &TeamDataService{store: store, signer: key, cfg: TeamDataConfig{InstallationID: "management-fixture", KeyVersion: "v1"}}
	a := teamDataPublishedMetadataFixture(t, store, s.cfg.InstallationID, key)
	b := teamDataPublishedMetadataFixture(t, store, s.cfg.InstallationID, key)
	// This fixture exercises the management state machine, not snapshot format
	// validation. Real data verification is separately covered by the pipeline.
	for _, item := range []PublishedArtifact{a, b} {
		if e := s.catalog().Import(context.Background(), item, 1, true); e != nil {
			t.Fatal(e)
		}
	}
	return s, a, b
}

func TestTeamDataDeleteRejectsExpiredConfirmationAndVersionDrift(t *testing.T) {
	s, a, _ := teamDataManagementFixture(t)
	ctx := context.Background()
	id := a.Manifest.Payload.BackupID
	if e := s.Protect(ctx, id, 1, false); e != nil {
		t.Fatal(e)
	}
	preview, e := s.DeletePreview(ctx, id, 1)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(preview.Confirmation)
	var token signedTeamDataDeleteConfirmation
	if e = strictDecode(raw, &token); e != nil {
		t.Fatal(e)
	}
	token.Payload.ExpiresAt = time.Now().Add(-time.Second)
	token.Signature, e = signObject("clawmanager:team-data-delete-confirmation:v1", token.Payload, s.signer, "v1")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = canonicalJSON(token)
	if _, e = s.catalog().BeginDelete(ctx, id, base64.RawURLEncoding.EncodeToString(raw), 1, 1); e == nil {
		t.Fatal("expired confirmation accepted")
	}
	key, _ := s.store.key(a.Commit.Payload.Manifest.Key)
	if _, e = s.store.Client.PutObject(ctx, s.store.Bucket, key, strings.NewReader("extra unexpected version"), 24, minio.PutObjectOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.catalog().BeginDelete(ctx, id, preview.Confirmation, 1, 1); e == nil {
		t.Fatal("version drift accepted")
	}
}

func TestTeamDataConfirmedDeletionJobIsIdempotent(t *testing.T) {
	s, a, _ := teamDataManagementFixture(t)
	jobs := teamDataJobFixture(t)
	s.db = jobs.db
	s.configHash = jobs.configHash
	ctx := context.Background()
	id := a.Manifest.Payload.BackupID
	if e := s.Protect(ctx, id, 1, false); e != nil {
		t.Fatal(e)
	}
	preview, e := s.DeletePreview(ctx, id, 1)
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.DeleteConfirmed(ctx, id, preview.Confirmation, "confirmed-delete-idempotent", 1)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.DeleteConfirmed(ctx, id, preview.Confirmation, "confirmed-delete-idempotent", 1)
	if e != nil || retry.ID != j.ID {
		t.Fatal("confirmation retry did not recover original job", e)
	}
	if e = s.Cancel(ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	resume, e := s.Create(ctx, 1, "resume-delete-after-cancel", "delete", id)
	if e != nil || resume.SourceID != id {
		t.Fatal("canceling delete lost durable intent", e)
	}
}

func TestTeamDataStorageCASSerializesLeaseAndDeletion(t *testing.T) {
	s, a, _ := teamDataManagementFixture(t)
	ctx := context.Background()
	id := a.Manifest.Payload.BackupID
	if e := s.Protect(ctx, id, 1, false); e != nil {
		t.Fatal(e)
	}
	preview, e := s.DeletePreview(ctx, id, 1)
	if e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var leaseErr, deleteErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		leaseErr = s.catalog().Acquire(ctx, id, "concurrent-restore", "owner")
	}()
	go func() {
		defer wg.Done()
		<-start
		_, deleteErr = s.catalog().BeginDelete(ctx, id, preview.Confirmation, 1, 1)
	}()
	close(start)
	wg.Wait()
	if (leaseErr == nil) == (deleteErr == nil) {
		t.Fatal("expected exactly one successful action", leaseErr, deleteErr)
	}
	state, _, e := s.catalog().Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	v := state.Artifacts[id]
	if v.Deleting && len(v.Leases) > 0 {
		t.Fatal("provider ignored conditional write; delete overlaps restore")
	}
}
func TestTeamDataDeleteConfirmationCannotMutateAnotherBackup(t *testing.T) {
	s, a, b := teamDataManagementFixture(t)
	ctx := context.Background()
	id := a.Manifest.Payload.BackupID
	if e := s.Protect(ctx, id, 1, false); e != nil {
		t.Fatal(e)
	}
	preview, e := s.DeletePreview(ctx, id, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DeleteConfirmed(ctx, b.Manifest.Payload.BackupID, preview.Confirmation, "wrong-target-confirmation", 1); e == nil {
		t.Fatal("target substitution accepted")
	}
	state, _, e := s.catalog().Load(ctx)
	if e != nil || state.Artifacts[id].Deleting {
		t.Fatal("wrong target mutated the confirmed backup", e)
	}
}
func TestTeamDataProtectedInUseAndMinimumRecoveryPoints(t *testing.T) {
	s, a, b := teamDataManagementFixture(t)
	ctx := context.Background()
	id := a.Manifest.Payload.BackupID
	if _, e := s.DeletePreview(ctx, id, 1); e == nil || e.Error() != "backup_protected" {
		t.Fatal("protected backup deletable", e)
	}
	if e := s.Protect(ctx, id, 1, false); e != nil {
		t.Fatal(e)
	}
	if e := s.catalog().Acquire(ctx, id, "active", "owner"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DeletePreview(ctx, id, 1); e == nil || e.Error() != "backup_in_use" {
		t.Fatal("active backup deletable", e)
	}
	if e := s.catalog().Release(ctx, id, "active", "owner"); e != nil {
		t.Fatal(e)
	}
	if e := s.catalog().Update(ctx, func(c *TeamDataCatalog) error {
		v := c.Artifacts[b.Manifest.Payload.BackupID]
		v.Health = "unavailable"
		c.Artifacts[v.BackupID] = v
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DeletePreview(ctx, id, 1); e == nil || e.Error() != "minimum_recovery_points" {
		t.Fatal("last recovery point deletable", e)
	}
}
func TestTeamDataExactVersionDeletionResumesAndRetainsTombstone(t *testing.T) {
	s, a, b := teamDataManagementFixture(t)
	ctx := context.Background()
	id := a.Manifest.Payload.BackupID
	if e := s.Protect(ctx, id, 7, false); e != nil {
		t.Fatal(e)
	}
	preview, e := s.DeletePreview(ctx, id, 7)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.catalog().BeginDelete(ctx, id, preview.Confirmation, 8, 1); e == nil {
		t.Fatal("actor substitution accepted")
	}
	if _, e = s.catalog().BeginDelete(ctx, id, preview.Confirmation[:len(preview.Confirmation)-5]+"aaaaa", 7, 1); e == nil {
		t.Fatal("tampered token accepted")
	}
	if _, e = s.catalog().BeginDelete(ctx, id, preview.Confirmation, 7, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.catalog().Acquire(ctx, id, "late-restore", "owner"); e == nil {
		t.Fatal("restore raced deletion")
	}
	stop := errors.New("injected transport interruption")
	e = s.catalog().ContinueDelete(ctx, id, 7, func(n int) error {
		if n == 1 {
			return stop
		}
		return nil
	})
	if !errors.Is(e, stop) {
		t.Fatal("partial failure not reported", e)
	}
	state, _, e := s.catalog().Load(ctx)
	if e != nil || state.Artifacts[id].DeletedVersions != 1 || !state.Artifacts[id].Deleting {
		t.Fatal("partial delete lost progress", e)
	}
	if e = s.catalog().ContinueDelete(ctx, id, 7, nil); e != nil {
		t.Fatal(e)
	}
	state, _, e = s.catalog().Load(ctx)
	if e != nil || !state.Artifacts[id].Deleted || state.Artifacts[id].DeletedVersions != preview.Versions {
		t.Fatal("delete incomplete", e)
	}
	if e = s.catalog().Import(ctx, a, 7, false); e == nil {
		t.Fatal("deleted backup reimported")
	}
	if _, e = OpenPublishedArtifact(ctx, s.store, s.cfg.InstallationID, b.Manifest.Payload.BackupID, s.catalog().PublicKey, "v1"); e != nil {
		t.Fatal("another backup was deleted", e)
	}
	if e = s.catalog().ContinueDelete(ctx, id, 7, nil); e == nil || !strings.Contains(e.Error(), "state_conflict") {
		t.Fatal("deleted operation restarted", e)
	}
}
