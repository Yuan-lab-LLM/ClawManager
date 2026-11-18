package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTeamDataDiscoveryCandidateStaysInsideInstallation(t *testing.T) {
	id := "sbk_0123456789abcdef0123456789abcdef"
	prefix := "team-data/install/"
	for _, tc := range []struct{ key, want string }{
		{prefix + id + "/", id},
		{prefix + "../other/", ""},
		{"team-data/foreign/" + id + "/", ""},
		{prefix + id + "/mysql/part", ""},
		{prefix + "sdr_0123456789abcdef0123456789abcdef/", ""},
	} {
		if got := teamDataDiscoveryCandidate(prefix, tc.key); got != tc.want {
			t.Errorf("candidate %q: got %q, want %q", tc.key, got, tc.want)
		}
	}
}

func teamDataPublishedMetadataFixture(t *testing.T, store ImmutableObjectStore, origin string, key ed25519.PrivateKey) PublishedArtifact {
	t.Helper()
	ctx := context.Background()
	id, _ := RandomID("sbk_")
	stage := t.TempDir()
	wrap := AESKeyWrapper{Reference: SecretRef{"kubernetes", "fixture-key", "v1", "encryption"}, Key: make([]byte, 32)}
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: id, OriginInstallationID: origin, Purpose: "team_data", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "best_effort"}
	m.ExcludedSQLTables = teamDataExcludedTables()
	for _, name := range RequiredParts {
		p, e := CapturePart(ctx, stage, origin, id, name, 1<<20, wrap, func(_ context.Context, w io.Writer) error {
			_, e := io.WriteString(w, "metadata-fixture-only")
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		m.Parts = append(m.Parts, p)
	}
	signed, e := SignManifest(m, key, "v1")
	if e != nil {
		t.Fatal(e)
	}
	a, e := PublishFunctionalArtifact(ctx, store, stage, signed, key, "v1", 16<<20)
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestTeamDataDiscoveryAndCatalogWithoutSourceDatabase(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	a := teamDataPublishedMetadataFixture(t, store, "source-gone", key)
	b := teamDataPublishedMetadataFixture(t, store, "source-gone", key)
	c := TeamDataCatalogStore{Store: store, Origin: "source-gone", PublicKey: pub, Signer: key, KeyVersion: "v1"}
	page, e := DiscoverTeamDataArtifacts(ctx, c, "", 1)
	if e != nil || len(page.Items) != 1 || !page.Partial || page.AuthoritativeSnapshot {
		t.Fatal("first page", e, page)
	}
	next, e := DiscoverTeamDataArtifacts(ctx, c, page.NextCursor, 1)
	if e != nil || len(next.Items) != 1 || next.Partial || next.Items[0].BackupID == page.Items[0].BackupID {
		t.Fatal("resume page", e, next)
	}
	if _, e = DiscoverTeamDataArtifacts(ctx, c, "foreign/backup/", 1); e == nil {
		t.Fatal("foreign cursor accepted")
	}
	if e = c.Import(ctx, a, 7, false); e != nil {
		t.Fatal(e)
	}
	if e = c.Import(ctx, b, 7, false); e != nil {
		t.Fatal(e)
	}
	state, _, e := c.Load(ctx)
	if e != nil || len(state.Artifacts) != 2 || !state.Artifacts[a.Manifest.Payload.BackupID].Protected || state.Artifacts[a.Manifest.Payload.BackupID].Health != "unchecked" {
		t.Fatal("import forged data verification", e, state)
	}
	id := a.Manifest.Payload.BackupID
	if e = c.Acquire(ctx, id, "run-a", "owner-a"); e != nil {
		t.Fatal(e)
	}
	if e = c.Release(ctx, id, "run-a", "foreign-owner"); e == nil {
		t.Fatal("foreign lease release")
	}
	if e = c.Release(ctx, id, "run-a", "owner-a"); e != nil {
		t.Fatal(e)
	}
	if e = c.Update(ctx, func(s *TeamDataCatalog) error {
		v := s.Artifacts[id]
		v.Deleting = true
		s.Artifacts[id] = v
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = c.Acquire(ctx, id, "run-b", "owner-b"); e == nil {
		t.Fatal("delete/restore race admitted")
	}
	if e = c.Import(ctx, a, 7, false); e == nil {
		t.Fatal("tombstone reimported")
	}
	all, e := DiscoverTeamDataArtifacts(ctx, c, "", 20)
	if e != nil || len(all.Items) != 1 || all.Items[0].BackupID != b.Manifest.Payload.BackupID {
		t.Fatal("deleting backup rediscovered", e, all)
	}
	_, _, e = TeamDataCatalogStore{Store: store, Origin: "source-gone", PublicKey: make([]byte, 32), KeyVersion: "v1"}.Load(ctx)
	if e == nil || !strings.Contains(e.Error(), "signature") {
		t.Fatal("catalog signature not checked", e)
	}
}

func TestTeamDataRestoreAcceptsImportedArtifactWithoutSourceJob(t *testing.T) {
	s := teamDataJobFixture(t)
	s.store = testObjectStore(t)
	s.cfg.InstallationID = "missing-source-job"
	s.cfg.KeyVersion = "v1"
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	s.signer = key
	a := teamDataPublishedMetadataFixture(t, s.store, s.cfg.InstallationID, key)
	if e := s.catalog().Import(context.Background(), a, 1, false); e != nil {
		t.Fatal(e)
	}
	var count int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM system_team_data_backup_jobs WHERE public_id=?", a.Manifest.Payload.BackupID).Scan(&count); e != nil || count != 0 {
		t.Fatal("fixture unexpectedly supplied source job", e)
	}
	j, e := s.Create(context.Background(), 1, "restore-import-without-source-job", "restore", a.Manifest.Payload.BackupID)
	if e != nil || j.SourceID != a.Manifest.Payload.BackupID || j.Status != "pending" {
		t.Fatal("imported artifact still depends on source job", e, j)
	}
	state, _, e := s.catalog().Load(context.Background())
	if e != nil || len(state.Artifacts[j.SourceID].Leases) != 1 {
		t.Fatal("queued restore is not protected from deletion", e)
	}
	if e = s.Cancel(context.Background(), j.ID); e != nil {
		t.Fatal(e)
	}
	state, _, e = s.catalog().Load(context.Background())
	if e != nil || len(state.Artifacts[j.SourceID].Leases) != 0 {
		t.Fatal("pending cancellation leaked reference", e)
	}
}
