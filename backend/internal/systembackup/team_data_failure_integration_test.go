package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

// This test creates only run-owned data copies and requires an explicitly
// provisioned recovery namespace. It never reads original application Secrets.
func TestTeamDataFailedRestoreRevokesBothWriters(t *testing.T) {
	if os.Getenv("CM_OWNED_RECOVERY_TESTS") != "1" {
		t.Skip("requires explicit owned recovery namespace")
	}
	ns := os.Getenv("POD_NAMESPACE")
	nsUID := os.Getenv("CM_OWNED_RECOVERY_NAMESPACE_UID")
	if ns == "" || nsUID == "" {
		t.Fatal("owned namespace identity missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	control := fullSchemaDB(t)
	store := testObjectStore(t)
	store.Root = "team-data"
	sourceObjects := testObjectStore(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	kek := make([]byte, 32)
	rand.Read(kek)
	wrap := AESKeyWrapper{Reference: SecretRef{"kubernetes", "team-data-backup-bootstrap", "v1", "encryption"}, Key: kek}
	origin := "failed-restore-fixture"
	id, _ := RandomID("sbk_")
	stage := t.TempDir()
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	source := CaptureSources{MySQL: control, Redis: redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT"), Objects: store.Client, ObjectBuckets: []string{sourceObjects.Bucket}, Workspace: []FileRoot{{ID: "fixture", Path: t.TempDir()}}, Namespace: ns, NamespaceUID: nsUID, DeploymentClient: client, SecretClient: client, TeamDataOnly: true}
	tables, e := snapshotBusinessTables(ctx, control, true)
	if e != nil {
		t.Fatal(e)
	}
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: id, OriginInstallationID: origin, Purpose: "team_data", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "best_effort", ExcludedSQLTables: teamDataExcludedTables()}
	for _, adapter := range captureAdapters(source, CaptureBudget{16 << 20, 10000}, tables) {
		capture := adapter.capture
		part, e := CapturePart(ctx, stage, origin, id, adapter.name, 16<<20, wrap, capture)
		if e != nil {
			t.Fatal(e)
		}
		m.Parts = append(m.Parts, part)
	}
	signed, e := SignManifest(m, key, "v1")
	if e != nil {
		t.Fatal(e)
	}
	artifact, e := PublishFunctionalArtifact(ctx, store, stage, signed, key, "v1", 16<<20)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
		return path
	}
	objectJSON, _ := json.Marshal(map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD")})
	readerJSON, _ := json.Marshal(map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_RESTORED_OBJECT_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_RESTORED_OBJECT_PASSWORD")})
	cfg := TeamDataConfig{InstallationID: origin, Namespace: ns, NamespaceUID: nsUID, SourceRoot: "/not-mounted-origin/source", RuntimeRoot: "/not-mounted-origin/runtime", StageRoot: filepath.Join(dir, "stage"), RestoreRoot: filepath.Join(dir, "restore"), SourceBucket: sourceObjects.Bucket, BackupBucket: store.Bucket, ObjectEndpoint: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT"), ObjectCredentialsFile: write("objects.json", objectJSON), VerifierObjectCredentialsFile: write("reader.json", readerJSON), AdminDSNFile: write("admin.dsn", []byte("root@tcp(127.0.0.1:"+os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT")+")/")), EncryptionKeyFile: write("encryption.key", kek), SigningKeyFile: write("signing.key", key), KeyVersion: "v1", RedisImage: os.Getenv("CM_OWNED_RECOVERY_REDIS_IMAGE"), MaxBytes: 16 << 20, MaxItems: 10000, DeadlineSeconds: 240}
	raw, _ := json.Marshal(cfg)
	s, e := NewTeamDataRecoveryService(ctx, control, write("service.json", raw))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	// The archive is valid and passes content preflight. Fail only after real
	// restore writing has started, while SQL/Redis writers already exist.
	// Reserving a file where the workspace directory must be created models a
	// target filesystem error, without introducing a production fault hook.
	injected := make(chan error, 1)
	poisonCtx, stopPoison := context.WithCancel(ctx)
	defer stopPoison()
	go func() {
		for poisonCtx.Err() == nil {
			entries, _ := os.ReadDir(cfg.RestoreRoot)
			for _, entry := range entries {
				if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "sdr_") {
					continue
				}
				copyDir := filepath.Join(cfg.RestoreRoot, entry.Name())
				if _, e := os.Stat(filepath.Join(copyDir, ".restore-owner")); e == nil {
					f, e := os.OpenFile(filepath.Join(copyDir, "workspace"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if e == nil {
						e = f.Close()
					}
					injected <- e
					return
				}
			}
			select {
			case <-poisonCtx.Done():
			case <-time.After(5 * time.Millisecond):
			}
		}
		injected <- poisonCtx.Err()
	}()
	job, e := s.Recover(ctx, origin, id, artifact.CommitReference.SHA256)
	if job.ID != "" {
		t.Cleanup(func() {
			clean, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if current, err := s.Get(clean, job.ID); err == nil && current.Status == "cleaned" {
				return
			}
			if err := s.CleanupRecovery(clean, job.ID); err != nil {
				t.Error("owned failed-copy cleanup", err)
			}
		})
	}
	if e == nil || job.Status != "failed" {
		t.Fatal("injected workspace failure was not reported", e, job.Status)
	}
	select {
	case e := <-injected:
		if e != nil {
			t.Fatal("write-stage injection failed", e)
		}
	case <-time.After(time.Second):
		t.Fatal("write stage never reached")
	}
	var report TeamDataReport
	if json.Unmarshal(job.Report, &report) != nil || report.Restore == nil || report.Restore.Phase != "workspace" {
		t.Fatal("failure occurred before intended boundary", string(job.Report))
	}
	secret, e := s.kube.Resource(teamDataSecretGVR).Namespace(ns).Get(ctx, teamDataRedisName(job.ID), metav1.GetOptions{})
	if e != nil {
		t.Fatal(e)
	}
	// The control record must never assert both writers were revoked while a
	// Redis writer still exists. Check the actual Redis ACL with its admin.
	var redis teamDataRedis
	redis.address = teamDataRedisName(job.ID) + "." + ns + ".svc:6379"
	var typed struct {
		Data map[string][]byte `json:"data"`
	}
	raw, _ = json.Marshal(secret.Object)
	if e = json.Unmarshal(raw, &typed); e != nil {
		t.Fatal(e)
	}
	redis.admin = string(typed.Data["admin"])
	redis.writer = string(typed.Data["writer"])
	admin, e := connectTeamRedis(ctx, redis, "admin")
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	user, e := admin.command(ctx, []byte("ACL"), []byte("GETUSER"), []byte("writer"))
	if e != nil {
		t.Fatal(e)
	}
	if user != nil {
		t.Errorf("Redis writer remains after restore failure; revoked flag=%t", report.WriteCredentialRevoked)
	}
	if !report.WriteCredentialRevoked {
		t.Error("failed restore did not confirm both temporary writers revoked")
	}
	var accounts int
	if e = s.admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE User=?", teamDataWriter(job.ID)).Scan(&accounts); e != nil || accounts != 0 {
		t.Fatal("SQL writer remains", e)
	}
	if !strings.Contains(string(job.Report), "workspace") {
		t.Fatal("failure report lost phase")
	}
	// Toggle the actual bootstrap, then reconstruct the service as an upgrade
	// would. Turning off new work must not change ownership of existing copies.
	cfg.Disabled = true
	raw, _ = json.Marshal(cfg)
	disabled, e := NewTeamDataRecoveryService(ctx, control, write("disabled-service.json", raw))
	if e != nil {
		t.Fatal("disabled bootstrap no longer supports cleanup", e)
	}
	defer disabled.Close()
	if disabled.configHash != s.configHash {
		t.Fatal("disable changed existing copy ownership")
	}
	if _, e = disabled.Create(ctx, 1, "disabled-new-restore", "restore", id); e == nil || e.Error() != "feature_disabled" {
		t.Fatal("disabled new work accepted", e)
	}
	if _, e = disabled.Get(ctx, job.ID); e != nil {
		t.Fatal("disabled history unavailable", e)
	}
	if e = disabled.CleanupRecovery(ctx, job.ID); e != nil {
		t.Fatal("disabled owned-copy cleanup failed", e)
	}
	t.Log("valid snapshot passed preflight; target filesystem write failure: failed status retained, original backup preserved, actual SQL/Redis writer identities checked")
}
