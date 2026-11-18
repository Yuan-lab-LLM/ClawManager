package systembackup

import (
	"context"
	"crypto/rand"
	"io"
	"math"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func publishTeamPreflightFixture(t *testing.T, s *TeamDataService, j TeamDataJob, corrupt string) PublishedArtifact {
	t.Helper()
	ctx := context.Background()
	sourceDB := fullSchemaDB(t)
	if corrupt == "schema" {
		if _, e := sourceDB.Exec("ALTER TABLE team_tasks ADD COLUMN unexpected_preflight_column TEXT NULL"); e != nil {
			t.Fatal(e)
		}
	}
	tables, e := snapshotBusinessTables(ctx, sourceDB, true)
	if e != nil {
		t.Fatal(e)
	}
	source := CaptureSources{MySQL: sourceDB, Redis: redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT"), Objects: s.objects, ObjectBuckets: []string{s.store.Bucket}, Workspace: []FileRoot{{ID: "fixture", Path: t.TempDir()}}, Namespace: s.cfg.Namespace, NamespaceUID: "fixture", DeploymentClient: s.kube, SecretClient: s.kube, TeamDataOnly: true}
	s.wrapper = AESKeyWrapper{Reference: SecretRef{"kubernetes", "fixture-key", "v1", "encryption"}, Key: make([]byte, 32)}
	rand.Read(s.wrapper.Key)
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: j.SourceID, OriginInstallationID: s.cfg.InstallationID, Purpose: "team_data", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "best_effort", ExcludedSQLTables: teamDataExcludedTables()}
	stage := t.TempDir()
	for _, a := range captureAdapters(source, CaptureBudget{16 << 20, 10000}, tables) {
		capture := a.capture
		if a.name == corrupt {
			capture = func(_ context.Context, w io.Writer) error {
				_, e := io.WriteString(w, "authenticated but malformed archive")
				return e
			}
		}
		part, e := CapturePart(ctx, stage, s.cfg.InstallationID, j.SourceID, a.name, 16<<20, s.wrapper, capture)
		if e != nil {
			t.Fatal(e)
		}
		m.Parts = append(m.Parts, part)
	}
	signed, e := SignManifest(m, s.signer, "v1")
	if e != nil {
		t.Fatal(e)
	}
	a, e := PublishFunctionalArtifact(ctx, s.store, stage, signed, s.signer, "v1", 16<<20)
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestTeamDataPreflightChecksContentCapacityAndMissingPart(t *testing.T) {
	s, j := teamDataProvisionFixture(t)
	s.cfg.MaxItems = 10000
	a := publishTeamPreflightFixture(t, s, j, "")
	ctx := context.Background()
	if e := s.preflightRestore(ctx, a); e != nil {
		t.Fatal("valid fixture rejected", e)
	}
	if n, e := availableRestoreBytes(s.cfg.RestoreRoot); e != nil || n == 0 {
		t.Fatal("real work-volume capacity unavailable", e)
	}
	if e := s.preflightRestoreWithSpace(ctx, a, 1); e == nil || e.Error() != "capacity_exceeded" {
		t.Fatal("insufficient capacity accepted", e)
	}
	// Row budget is enforced while parsing, not only as a cipher-size limit.
	s.cfg.MaxItems = 1
	if e := s.preflightRestoreWithSpace(ctx, a, math.MaxUint64); e == nil {
		t.Fatal("item budget ignored")
	}
	s.cfg.MaxItems = 10000
	// Delete exactly one fixture part; the original retained backup is never touched.
	var ref ObjectReference
	for _, p := range a.Index.Payload.Parts {
		if p.Name == "workspace" {
			ref = p.Objects[0]
		}
	}
	if ref.Key == "" {
		t.Fatal("workspace reference missing in fixture")
	}
	key, e := s.store.key(ref.Key)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.store.Client.RemoveObject(ctx, s.store.Bucket, key, minio.RemoveObjectOptions{VersionID: ref.VersionID}); e != nil {
		t.Fatal(e)
	}
	if e := s.preflightRestore(ctx, a); e == nil {
		t.Fatal("missing part accepted")
	}
	var count int
	if e := s.admin.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", teamDataDatabase(j.ID)).Scan(&count); e != nil || count != 0 {
		t.Fatal("preflight allocated target", e, count)
	}
}

func TestTeamDataBadContentRejectedBeforeProvisioning(t *testing.T) {
	for _, corrupt := range []string{"workspace", "schema"} {
		t.Run(corrupt, func(t *testing.T) {
			s, j := teamDataProvisionFixture(t)
			s.cfg.MaxItems = 10000
			s.verifierObjects = s.objects
			publishTeamPreflightFixture(t, s, j, corrupt)
			_, e := s.restore(context.Background(), j)
			if e == nil {
				t.Fatal("bad snapshot accepted")
			}
			var exists int
			if e := s.admin.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", teamDataDatabase(j.ID)).Scan(&exists); e != nil || exists != 0 {
				t.Fatal("bad content allocated restore database", e, exists)
			}
			var receipt []byte
			if e := s.db.QueryRow("SELECT provisioning_json FROM system_team_data_backup_jobs WHERE public_id=?", j.ID).Scan(&receipt); e != nil || len(receipt) != 0 {
				t.Fatal("bad content entered provisioning", e, string(receipt))
			}
		})
	}
}
