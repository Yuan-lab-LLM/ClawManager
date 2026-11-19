package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appdb "clawreef/internal/db"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/observability"
	"clawreef/internal/repository"
	"github.com/minio/minio-go/v7"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func (s historyTestObjects) DeleteObservabilityObject(ctx context.Context, key string) error {
	return s.store.Client.RemoveObject(ctx, s.store.Bucket, s.prefix+key, minio.RemoveObjectOptions{})
}

type lostDeleteAck struct {
	historyTestObjects
	once bool
}

func (s *lostDeleteAck) DeleteObservabilityObject(ctx context.Context, key string) error {
	if err := s.historyTestObjects.DeleteObservabilityObject(ctx, key); err != nil {
		return err
	}
	if s.once {
		s.once = false
		return errors.New("injected lost MinIO delete acknowledgement")
	}
	return nil
}

func TestObservationNativeProofAndExpiredSnapshot(t *testing.T) {
	exe := os.Getenv("TEST_SYSTEM_BACKUP_VERIFY_EXE")
	if exe == "" {
		t.Skip("requires system-backup-artifact executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db := fullSchemaDB(t)
	origin := "observation-proof-fixture"
	control := Store{DB: db, InstallationID: origin}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(control.Bootstrap(ctx))
	must((Gate{db}).EnsureScope(ctx, origin))
	var database string
	must(db.QueryRow("SELECT DATABASE()").Scan(&database))
	dir := t.TempDir()
	write := func(name string, raw []byte) string {
		p := filepath.Join(dir, name)
		must(os.WriteFile(p, raw, 0600))
		return p
	}
	jsonFile := func(name string, v any) string { raw, e := json.Marshal(v); must(e); return write(name, raw) }
	sourceDB := write("source.dsn", []byte(fmt.Sprintf("root:%s@tcp(127.0.0.1:%s)/%s?parseTime=true", os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PASSWORD"), os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), database)))
	sourceStore, archive := testObjectStore(t), testObjectStore(t)
	objects := historyTestObjects{sourceStore, "cm/native/"}
	key := bytes.Repeat([]byte{27}, 32)
	fault := &lostDeleteAck{historyTestObjects: objects}
	encrypted, e := observability.NewEncryptedObjects(fault, key)
	must(e)
	journalDir := t.TempDir()
	journal, e := observability.NewJournal(journalDir, key, observability.Limits{RequestBytes: 1 << 20})
	must(e)
	undo := repository.ConfigureObservability(appdb.Session, journal, encrypted)
	defer undo()
	body, e := encrypted.Put(ctx, "user:42", []byte(`{"details_body_ref":"user text, consumed only by the application"}`))
	must(e)
	orphan, e := encrypted.Put(ctx, "user:43", []byte("uploaded before projection"))
	must(e)
	lateHeld, e := encrypted.Put(ctx, body.Owner, []byte("referenced by a new archive after proof"))
	must(e)
	drift, e := encrypted.Put(ctx, "user:45", []byte("source identity changed"))
	must(e)
	missingIntent := encrypted.Reference("user:44", []byte("upload never succeeded"))
	held, e := encrypted.Put(ctx, body.Owner, []byte("Team archive child with the same owner as expired detail"))
	must(e)
	archiveBytes, _ := json.Marshal(map[string]any{"id": 17, "team_id": 42, "content_body_ref": held})
	archiveRef, e := encrypted.Put(ctx, body.Owner, archiveBytes)
	must(e)
	for _, ref := range []observability.BodyRef{body, orphan, missingIntent, held, archiveRef, lateHeld, drift} {
		_, e = db.Exec("INSERT INTO observability_objects(object_key,owner,sha256,byte_length,encryption_version,retain_until) VALUES(?,?,?,?,1,UTC_TIMESTAMP()-INTERVAL 40 DAY)", ref.ObjectKey, ref.Owner, ref.SHA256, ref.ByteLength)
		must(e)
	}
	must(historymaintenance.New(db, nil, nil).PrepareMonths(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
	ar, _ := json.Marshal(archiveRef)
	_, e = db.Exec(`INSERT INTO history_archive_records(kind,original_id,owner_id,team_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at) VALUES('team_events',17,42,42,'2026-11-02','team_events_202611',?,?,?,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, string(ar), archiveRef.SHA256, archiveRef.ByteLength)
	must(e)
	_, e = db.Exec("INSERT INTO team_events_202611(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes) VALUES(17,42,'2026-11-02','',0,?,?,?)", string(ar), archiveRef.SHA256, archiveRef.ByteLength)
	must(e)
	uid := strings.Repeat("1", 64)
	raw, _ := json.Marshal(body)
	_, e = db.Exec("INSERT INTO audit_events(trace_id,event_type,traffic_class,severity,message,user_id,event_uid,details_body_ref) VALUES('obs-proof','sample','user','info','fixture',42,?,?)", uid, string(raw))
	must(e)
	_, e = db.Exec("INSERT INTO observability_body_refs(source_table,event_uid,field_name,object_key,body_ref,created_at) VALUES('audit_events',?,'Details',?,?,UTC_TIMESTAMP()-INTERVAL 40 DAY)", uid, body.ObjectKey, string(raw))
	must(e)
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": observationKeySecret, "namespace": "proof", "uid": "key-uid"}, "immutable": true, "data": map[string]any{"master-key": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(key)))}}}
	kube := fake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
	src := CaptureSources{MySQL: db, Redis: redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT"), Objects: sourceStore.Client, ObjectBuckets: []string{sourceStore.Bucket}, Workspace: []FileRoot{{ID: "observability", Path: journalDir}}, DeploymentClient: kube, SecretClient: kube, Namespace: "proof", NamespaceUID: "proof-uid", Secrets: []KubernetesSelection{{Version: "v1", Resource: "secrets", Name: observationKeySecret, UID: "key-uid"}}}
	pub, signer, e := ed25519.GenerateKey(rand.Reader)
	must(e)
	wrapper := AESKeyWrapper{Reference: SecretRef{Provider: "kubernetes", Name: "proof-bootstrap", Version: "v1", Purpose: "encryption"}, Key: bytes.Repeat([]byte{4}, 32)}
	b := CaptureBudget{64 << 20, 10000}
	capture := func() PublishedArtifact {
		t.Helper()
		id, _ := RandomID("sbk_")
		stage := t.TempDir()
		signed, e := CaptureFunctionalBundle(ctx, stage, origin, id, src, b, wrapper, signer, "v1")
		must(e)
		a, e := PublishFunctionalArtifact(ctx, archive, stage, signed, signer, "v1", b.MaxBytes)
		must(e)
		return a
	}
	endpoint := "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT")
	cred := jsonFile("objects.json", map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD")})
	bootstrap := jsonFile("bootstrap.json", map[string]any{"endpoint": endpoint, "bucket": archive.Bucket, "root": archive.Root, "credentials_file": cred, "verification_key_file": write("pub", pub), "verification_key_version": "v1", "encryption_key_file": write("key", wrapper.Key), "encryption_reference": wrapper.Reference})
	restore := func(a PublishedArtifact) (*exec.Cmd, string, func()) {
		t.Helper()
		must(ValidatePublishedSnapshot(ctx, archive, a, wrapper, b))
		target, name := isolatedSchemaDB(t, db)
		reader, dsn := selectOnlyVerifierWithDSN(t, db, name)
		bucket, _ := RandomID("cm-restore-")
		must(sourceStore.Client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}))
		t.Cleanup(func() {
			for item := range sourceStore.Client.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Recursive: true}) {
				sourceStore.Client.RemoveObject(context.Background(), bucket, item.Key, minio.RemoveObjectOptions{})
			}
			sourceStore.Client.RemoveBucket(context.Background(), bucket)
		})
		run, _ := RandomID("sdr_")
		rdb := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT")
		clearRedis := func() {
			cursor := []byte("0")
			for {
				v, e := rdb.command(context.Background(), []byte("SCAN"), cursor, []byte("MATCH"), []byte("cm:restore-history:"+run+":*"), []byte("COUNT"), []byte("200"))
				if e != nil {
					t.Error(e)
					return
				}
				p := v.([]any)
				cursor = p[0].([]byte)
				for _, k := range p[1].([]any) {
					_, e = rdb.command(context.Background(), []byte("DEL"), k.([]byte))
					if e != nil {
						t.Error(e)
						return
					}
				}
				if string(cursor) == "0" {
					return
				}
			}
		}
		t.Cleanup(clearRedis)
		directory := t.TempDir()
		mapping := map[string]string{sourceStore.Bucket: bucket}
		targets := DataRestoreTarget{MySQL: target, Verifier: reader, Database: name, Redis: rdb, Objects: sourceStore.Client, BucketMapping: mapping, Directory: directory, Control: Store{DB: controlDB(t), InstallationID: "restore-control"}}
		report, e := RestorePublishedData(ctx, archive, a, wrapper, run, targets, b)
		must(e)
		intent := RestoreVerificationIntent{RunID: run, NormalizedAt: report.StartedAt, HistoryParts: report.HistoryParts}
		verifier := jsonFile("verify-"+run, map[string]any{"intent": intent, "verifier_dsn_file": write("reader-"+run, []byte(dsn)), "objects": map[string]any{"endpoint": endpoint, "credentials_file": cred}, "redis": map[string]any{"address": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT"), "database": redisFixtureDatabase(t), "credentials_file": jsonFile("redis.json", map[string]string{"password": os.Getenv("TEST_SYSTEM_BACKUP_REDIS_PASSWORD")})}, "bucket_mapping": mapping, "directory": directory})
		op := jsonFile("proof-"+run, map[string]string{"verification_config_file": verifier, "source_dsn_file": sourceDB, "control_dsn_file": sourceDB})
		cmd := exec.CommandContext(ctx, exe, "--operation", "record-observation-restore-proof", "--bootstrap", bootstrap, "--origin", origin, "--backup", a.Manifest.Payload.BackupID, "--operation-config", op, "--max-bytes", "67108864", "--max-items", "10000")
		cmd.Env = append(os.Environ(), "SYSTEM_BACKUP_SQL_GATE_SCOPE="+origin)
		return cmd, name, clearRedis
	}
	before := capture()
	cmd, _, clearRedis := restore(before)
	gate := Gate{db}
	fence, e := gate.Begin(ctx, origin, "owned-test-gate", RegistryHash(), time.Minute, time.Minute)
	must(e)
	fence, e = gate.Hold(ctx, fence)
	must(e)
	again := func() *exec.Cmd { c := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...); c.Env = cmd.Env; return c }
	if out, err := again().CombinedOutput(); err == nil {
		t.Fatalf("held gate admitted proof: %s", out)
	}
	var proven int
	must(db.QueryRow("SELECT COUNT(*) FROM observability_objects WHERE restore_verified_at IS NOT NULL").Scan(&proven))
	if proven != 0 {
		t.Fatal("held gate wrote proof")
	}
	must(gate.BeginRelease(ctx, fence))
	must(gate.FinishRelease(ctx, fence))
	_, e = db.Exec("UPDATE observability_objects SET owner=UPPER(owner) WHERE object_key=?", drift.ObjectKey)
	must(e)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("native proof completion CLI: %v %s", e, out)
	}
	must(db.QueryRow("SELECT COUNT(*) FROM observability_objects WHERE backup_verified_at IS NOT NULL AND restore_verified_at IS NOT NULL").Scan(&proven))
	if proven != 5 {
		t.Fatalf("expected five exact source identities, got %d", proven)
	}
	_, e = db.Exec("UPDATE observability_objects SET owner=? WHERE object_key=? AND restore_verified_at IS NULL", drift.Owner, drift.ObjectKey)
	must(e)
	lateRaw, _ := json.Marshal(map[string]any{"body_ref": lateHeld})
	_, e = repository.DurableArchivePut(ctx, lateHeld.Owner, lateRaw)
	must(e)
	// Lose the acknowledgement specifically for the canonical body delete.
	_, e = db.Exec("UPDATE observability_objects SET retain_until=UTC_TIMESTAMP()-INTERVAL 41 DAY WHERE object_key=?", body.ObjectKey)
	must(e)
	fault.once = true
	if _, err := repository.ReclaimObservationObjects(ctx, 500); err == nil {
		t.Fatal("fixture did not lose delete acknowledgement")
	}
	interrupted := capture()
	if err := ValidatePublishedSnapshot(ctx, archive, interrupted, wrapper, b); err != nil {
		t.Fatalf("backup after lost body deletion acknowledgement: %v", err)
	}
	n, e := repository.ReclaimObservationObjects(ctx, 500)
	must(e)
	if n != 2 {
		t.Fatalf("native GC released %d, want 2", n)
	}
	var hidden bool
	var historicalBytes int64
	for item := range sourceStore.Client.ListObjects(ctx, sourceStore.Bucket, minio.ListObjectsOptions{Prefix: objects.prefix + body.ObjectKey, Recursive: true, WithVersions: true}) {
		must(item.Err)
		if item.Key == objects.prefix+body.ObjectKey {
			hidden = hidden || (item.IsLatest && item.IsDeleteMarker)
			if !item.IsLatest && !item.IsDeleteMarker {
				historicalBytes += item.Size
			}
		}
	}
	if !hidden || historicalBytes == 0 {
		t.Fatal("versioned fixture must retain noncurrent bytes beneath the live-key delete marker")
	}
	_, e = encrypted.Get(ctx, held.Owner, held)
	must(e)
	_, e = encrypted.Get(ctx, lateHeld.Owner, lateHeld)
	must(e)
	_, e = encrypted.Get(ctx, archiveRef.Owner, archiveRef)
	must(e)
	clearRedis()
	// The history worker captures SQL JSON columns as strings after 90 days.
	// An explicitly expired audit body must remain readable as metadata.
	expiredRaw, _ := json.Marshal(map[string]any{"id": 1, "event_uid": uid, "user_id": 42, "details_body_ref": string(raw)})
	expiredArchive, e := repository.DurableHistoryArchivePut(ctx, body.Owner, "history/audit_events/1/fixture", expiredRaw)
	must(e)
	var expiredRef observability.BodyRef
	must(json.Unmarshal(expiredArchive, &expiredRef))
	_, e = db.Exec(`INSERT INTO history_archive_records(kind,original_id,owner_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at) VALUES('audit_events',1,42,'2026-11-02','audit_events_202611',?,?,?,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, string(expiredArchive), expiredRef.SHA256, expiredRef.ByteLength)
	must(e)
	_, e = db.Exec("INSERT INTO audit_events_202611(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes) VALUES(1,42,'2026-11-02','',0,?,?,?)", string(expiredArchive), expiredRef.SHA256, expiredRef.ByteLength)
	must(e)
	detail, e := repository.HydrateArchivedPayload(ctx, "audit_events", expiredRaw)
	must(e)
	if !bytes.Contains(detail, []byte(`"Details":"expired"`)) {
		t.Fatal("archived detail lost explicit body expiry")
	}
	after := capture()
	cmd, name, _ := restore(after)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("post-expiry verification: %v %s", e, out)
	}
	var expiry int
	must(db.QueryRow("SELECT COUNT(*) FROM `" + name + "`.audit_events e JOIN `" + name + "`.observability_body_refs r ON r.event_uid=e.event_uid JOIN `" + name + "`.observability_objects o ON o.object_key=r.object_key WHERE r.released_at IS NOT NULL AND o.deleted_at IS NOT NULL AND e.details_body_ref=r.body_ref").Scan(&expiry))
	if expiry != 1 {
		t.Fatal("restored detail lost the explicit expired identity")
	}
	// The source opens an upload intent before PUT. An outage at that point
	// must preserve a prior event's closed release for the same content key.
	_, e = db.Exec("UPDATE observability_objects SET deleted_at=NULL,retain_until=UTC_TIMESTAMP()+INTERVAL 30 DAY WHERE object_key=?", body.ObjectKey)
	must(e)
	must(ValidatePublishedSnapshot(ctx, archive, capture(), wrapper, b))
	_, e = db.Exec("UPDATE observability_body_refs SET field_name='AnotherField' WHERE object_key=?", body.ObjectKey)
	must(e)
	unmatched := capture()
	if e := ValidatePublishedSnapshot(ctx, archive, unmatched, wrapper, b); e == nil {
		t.Fatal("absent live body silently approved through unrelated released field")
	}
}
