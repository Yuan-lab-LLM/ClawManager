package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"

	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestSixRealSourcesPublishAndDataOnlyRestore(t *testing.T) {
	budget := CaptureBudget{64 << 20, 10000}
	timeout := 3 * time.Minute
	if os.Getenv("TEST_SYSTEM_BACKUP_CONTROLLER_JOB") == "1" {
		timeout = 5 * time.Minute
	}
	if os.Getenv("TEST_SYSTEM_BACKUP_LARGE_SMOKE") == "1" {
		budget = CaptureBudget{4 << 30, 200000}
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var source CaptureSources
	teamData := os.Getenv("TEST_SYSTEM_TEAM_DATA_MODE") == "1"
	if teamData {
		client := fake.NewSimpleDynamicClient(runtime.NewScheme())
		source = CaptureSources{Namespace: "team-data-fixture", NamespaceUID: "fixture-namespace", DeploymentClient: client, SecretClient: client, TeamDataOnly: true}
	} else {
		source = realKubernetesCaptureSources(t)
	}
	source.MySQL = fullSchemaDB(t)
	source.Redis = redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT")
	sourceObjects := testObjectStore(t)
	external := testObjectStore(t)
	source.Objects = sourceObjects.Client
	source.ObjectBuckets = []string{sourceObjects.Bucket}
	root := t.TempDir()
	stage := t.TempDir()
	source.Workspace = []FileRoot{{ID: "owner_x", Path: root}}
	payload := []byte("interrupted task artifact\x00\xff\n")
	if e := os.MkdirAll(filepath.Join(root, ".state"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"partial.bin", ".state/active-assignment.json"} {
		if e := os.WriteFile(filepath.Join(root, p), payload, 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []string{`INSERT INTO users(id,username,email,password_hash,role) VALUES(901,'backup-provider','backup@test.invalid','disabled','user')`, `INSERT INTO teams(id,user_id,name,status,resource_scope,owner) VALUES(901,901,'backup-owner','running','northbound','owner-x')`, `INSERT INTO team_members(id,team_id,user_id,member_key,display_name,role,status) VALUES(901,901,901,'leader','Leader','leader','busy')`, `INSERT INTO team_tasks(id,team_id,target_member_id,message_id,status,payload_json,result_json,workflow_state) VALUES(901,901,901,'backup-task','running','{}','{"partial":"preserve this result"}','executing')`} {
		if _, e := source.MySQL.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := source.Objects.PutObject(ctx, sourceObjects.Bucket, "team-901/partial.bin", bytes.NewReader(payload), int64(len(payload)), minio.PutObjectOptions{}); e != nil {
		t.Fatal(e)
	}
	redisKey, _ := RandomID("pipeline_")
	if teamData {
		redisKey = "claw:team:901:backup:" + redisKey
	}
	key := []byte(redisKey)
	if _, e := source.Redis.command(ctx, []byte("SET"), key, payload); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("TEST_SYSTEM_BACKUP_LARGE_SMOKE") == "1" {
		populateLargeSmoke(t, ctx, source, root, sourceObjects.Bucket, redisKey)
	}
	run, _ := RandomID("sdr_")
	redisTarget := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT")
	t.Cleanup(func() {
		source.Redis.command(context.Background(), []byte("DEL"), key)
		// A snapshot can contain other fixture records. Clean every historical
		// key owned by this restore run, never another run or the source keys.
		prefix := "cm:restore-history:" + run + ":"
		cursor := []byte("0")
		var owned [][]byte
		for {
			v, e := redisTarget.command(context.Background(), []byte("SCAN"), cursor, []byte("MATCH"), []byte(prefix+"*"), []byte("COUNT"), []byte("100"))
			if e != nil {
				t.Error(e)
				break
			}
			page := v.([]any)
			cursor = page[0].([]byte)
			for _, v := range page[1].([]any) {
				k := v.([]byte)
				if bytes.HasPrefix(k, []byte(prefix)) {
					owned = append(owned, k)
				}
			}
			if string(cursor) == "0" {
				break
			}
		}
		for start := 0; start < len(owned); start += 1000 {
			end := min(start+1000, len(owned))
			args := append([][]byte{[]byte("DEL")}, owned[start:end]...)
			if _, e := redisTarget.command(context.Background(), args...); e != nil {
				t.Error(e)
			}
		}
	})
	id, _ := RandomID("sbk_")
	kek := make([]byte, 32)
	rand.Read(kek)
	wrapper := AESKeyWrapper{SecretRef{"kubernetes", "pipeline-key", "v1", "encryption"}, kek}
	pub, signer, _ := ed25519.GenerateKey(rand.Reader)
	var published PublishedArtifact
	var e error
	if exe := os.Getenv("TEST_SYSTEM_BACKUP_VERIFY_EXE"); exe != "" {
		runCaptureCLI(t, ctx, exe, external, source, stage, "fixture-installation", id, wrapper, pub, signer)
		published, e = OpenPublishedArtifact(ctx, external, "fixture-installation", id, pub, "v1")
		if e != nil {
			t.Fatal(e)
		}
	} else {
		signed, e := CaptureFunctionalBundle(ctx, stage, "fixture-installation", id, source, budget, wrapper, signer, "v1")
		if e != nil {
			t.Fatal("six-part capture:", e)
		}
		published, e = PublishFunctionalArtifact(ctx, external, stage, signed, signer, "v1", budget.MaxBytes)
		if e != nil {
			t.Fatal(e)
		}
		for _, name := range RequiredParts {
			if e = os.Remove(filepath.Join(stage, name+".enc")); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = ValidatePublishedSnapshot(ctx, external, published, wrapper, budget); e != nil {
		t.Fatal("independent snapshot format validation:", PublicError(e))
	}
	t.Log("capture and publication complete")
	// From here no source adapter is supplied to any restoration function.
	recovered, e := OpenPublishedArtifact(ctx, external, "fixture-installation", id, pub, "v1")
	if e != nil {
		t.Fatal(e)
	}
	if e = VerifyPublishedArtifact(ctx, external, recovered, wrapper, budget.MaxBytes); e != nil {
		t.Fatal(e)
	}
	t.Log("independent artifact integrity verified")
	runRealVerifyJob(t, ctx, external, recovered, wrapper, pub)
	target, targetName := isolatedSchemaDB(t, source.MySQL)
	control := Store{DB: controlDB(t), InstallationID: "restore-controller"}
	verifier, verifierDSN := selectOnlyVerifierWithDSN(t, source.MySQL, targetName)
	workspaceTarget := t.TempDir()
	targetBucket, _ := RandomID("cm-restore-")
	if e = source.Objects.MakeBucket(ctx, targetBucket, minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for o := range source.Objects.ListObjects(context.Background(), targetBucket, minio.ListObjectsOptions{Recursive: true}) {
			if o.Err == nil {
				source.Objects.RemoveObject(context.Background(), targetBucket, o.Key, minio.RemoveObjectOptions{})
			}
		}
		source.Objects.RemoveBucket(context.Background(), targetBucket)
	})
	restoreTarget := DataRestoreTarget{MySQL: target, Verifier: verifier, Database: targetName, Redis: redisTarget, Objects: source.Objects, BucketMapping: map[string]string{sourceObjects.Bucket: targetBucket}, Directory: workspaceTarget, Control: control}
	// Exercise the production writer/reader separation during restoration,
	// rather than discovering permission errors only in the final verifier.
	password, _ := RandomID("")
	for _, role := range []string{"writer", "reader"} {
		for _, line := range strings.Split(teamDataRedisConfig(teamDataRedis{writer: password, reader: password}, run), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[0] != "user" || fields[1] != role {
				continue
			}
			user := run + "_" + role
			args := [][]byte{[]byte("ACL"), []byte("SETUSER"), []byte(user)}
			for _, field := range fields[2:] {
				args = append(args, []byte(field))
			}
			if _, err := redisTarget.command(ctx, args...); err != nil {
				t.Fatal(err)
			}
			defer redisTarget.command(context.Background(), []byte("ACL"), []byte("DELUSER"), []byte(user))
			client, err := ConnectRedis(ctx, RedisOptions{Database: redisFixtureDatabase(t), Address: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT"), Username: user, Password: password, MaxReplyBytes: 64 << 20})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if role == "writer" {
				restoreTarget.Redis = client
			} else {
				restoreTarget.RedisVerifier = client
			}
		}
	}
	var report DataRestoreReport
	if exe := os.Getenv("TEST_SYSTEM_BACKUP_VERIFY_EXE"); exe != "" {
		report = runRestoreCLI(t, ctx, exe, external, recovered, wrapper, pub, run, restoreTarget, verifierDSN)
	} else {
		report, e = RestorePublishedData(ctx, external, recovered, wrapper, run, restoreTarget, budget)
	}
	if e != nil || report.Status != "succeeded" || report.Phase != "complete" || report.AcceptanceEligible || len(report.SQL.Tables) != 75 || len(report.HistoryParts) != 2 {
		t.Fatal("data restore runner", report.Phase, e)
	}
	t.Logf("restored and read back: files=%d objects=%d redis=%d", report.VerifiedFileCount, report.VerifiedObjectCount, report.VerifiedRedisCount)
	if os.Getenv("TEST_SYSTEM_BACKUP_LARGE_SMOKE") == "1" {
		if report.SQL.Tables["team_events"] != 100000 || report.VerifiedObjectCount != 10001 || report.VerifiedRedisCount != 100001 || report.VerifiedFileCount < 2050 {
			t.Fatal("large fixture count mismatch", report.SQL.Tables["team_events"], report.VerifiedObjectCount, report.VerifiedRedisCount, report.VerifiedFileCount)
		}
	}
	if !report.Team.SQLReadbackVerified {
		t.Fatal("Team report lacks SQL readback")
	}
	foundTransition := false
	for _, state := range report.Team.States {
		if state.Table == "team_tasks" && state.Field == "status" && state.Captured != nil && *state.Captured == "running" && state.Restored != nil && *state.Restored == "failed" && state.Rows == 1 {
			foundTransition = true
		}
	}
	if !foundTransition {
		t.Fatal("Team captured/restored state report missing")
	}
	var status, result string
	if e = verifier.QueryRow("SELECT status,result_json FROM team_tasks WHERE id=901").Scan(&status, &result); e != nil || status != "failed" || result != "{\"partial\":\"preserve this result\"}" {
		t.Fatal("interrupted task data lost", status, e)
	}
	got, e := os.ReadFile(filepath.Join(workspaceTarget, "workspace", "roots", "owner_x", "partial.bin"))
	if e != nil || !bytes.Equal(got, payload) {
		t.Fatal("workspace artifact lost", e)
	}
	if n, e := redisTarget.command(ctx, []byte("EXISTS"), key); e != nil || n != int64(0) {
		t.Fatal("old Redis task namespace reactivated")
	}
	raw, e := os.ReadFile(filepath.Join(workspaceTarget, "restore-report.json"))
	var saved DataRestoreReport
	if e != nil || json.Unmarshal(raw, &saved) != nil || saved.Status != "succeeded" || !teamData && saved.HistoryCounts["secret_bundle"] == 0 {
		t.Fatal("restore report missing", e)
	}
	if _, e = RestorePublishedData(ctx, external, recovered, wrapper, run, restoreTarget, budget); e == nil {
		t.Fatal("restore retry overwrote owned targets")
	}
	verifyTarget := restoreTarget
	verifyTarget.Redis = redisTarget // Fixture admin provisions a separate verifier ACL.
	verifyRestoredWithReadOnlyAccounts(t, ctx, external, recovered, wrapper, pub, report, verifyTarget, verifierDSN, budget)
	expectedPurpose := "functional_test"
	if teamData {
		expectedPurpose = "team_data"
	}
	if published.Manifest.Payload.Consistency != "best_effort" || published.Manifest.Payload.Purpose != expectedPurpose {
		t.Fatal("unproven strong backup claimed")
	}
}

func TestTeamDataSourcesPublishAndDataOnlyRestore(t *testing.T) {
	t.Setenv("TEST_SYSTEM_TEAM_DATA_MODE", "1")
	TestSixRealSourcesPublishAndDataOnlyRestore(t)
}
