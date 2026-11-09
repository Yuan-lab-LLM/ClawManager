package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	appdb "clawreef/internal/db"
	"github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func teamDataProvisionFixture(t *testing.T) (*TeamDataService, TeamDataJob) {
	t.Helper()
	s := teamDataJobFixture(t)
	s.cfg.InstallationID, s.cfg.KeyVersion, s.cfg.Namespace = "provision-fixture", "v1", "fixture"
	s.cfg.RestoreRoot = t.TempDir()
	s.cfg.MaxBytes = 16 << 20
	_, s.signer, _ = ed25519.GenerateKey(rand.Reader)
	s.store = testObjectStore(t)
	s.objects = s.store.Client
	s.kube = fake.NewSimpleDynamicClient(runtime.NewScheme())
	s.adminConfig = &mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), AllowNativePasswords: true, ParseTime: true}
	var e error
	s.admin, e = sql.Open("mysql", s.adminConfig.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.admin.Close() })
	id, _ := RandomID("sdr_")
	source, _ := RandomID("sbk_")
	_, e = s.db.Exec("INSERT INTO system_team_data_backup_jobs(public_id,operation,source_backup_id,actor_id,idempotency_key,request_hash,config_hash,status,owner_id) VALUES(?,'restore',?,1,?,?,?,'running',?)", id, source, id, s.configHash, s.configHash, s.owner)
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.Get(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		// Disposable fixture only: remove even when the regression fails.
		_, _ = s.admin.Exec("DROP DATABASE IF EXISTS `" + teamDataDatabase(id) + "`")
		_ = s.objects.RemoveBucket(context.Background(), teamDataBucket(id))
	})
	return s, j
}

func finishFailedProvision(t *testing.T, s *TeamDataService, j TeamDataJob) {
	t.Helper()
	if _, e := s.db.Exec("UPDATE system_team_data_backup_jobs SET status='failed' WHERE public_id=?", j.ID); e != nil {
		t.Fatal(e)
	}
}

func TestTeamDataSchemaPreparationFailureCanBeCleaned(t *testing.T) {
	s, j := teamDataProvisionFixture(t)
	_, e := s.prepareRestoreDatabase(context.Background(), j, func(ctx context.Context, d *sql.DB) error {
		// Simulate a partly installed schema, then exercise its real refusal.
		if _, e := d.ExecContext(ctx, "CREATE TABLE partial_schema(id INT)"); e != nil {
			return e
		}
		return appdb.PrepareIsolatedRestoreSchema(ctx, d)
	})
	if e == nil || !strings.Contains(e.Error(), "nonempty") {
		t.Fatal("schema failure not reached", e)
	}
	finishFailedProvision(t, s, j)
	if e = s.cleanup(context.Background(), j.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.cleanup(context.Background(), j.ID); e != nil {
		t.Fatal("repeat cleanup", e)
	}
}

type provisionTagFailureTransport struct{}

func (provisionTagFailureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "PUT" && r.URL.Query().Has("tagging") {
		return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader("<Error><Code>AccessDenied</Code><Message>injected tagging failure</Message></Error>")), Request: r}, nil
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestTeamDataBucketTagFailureCanBeCleaned(t *testing.T) {
	s, j := teamDataProvisionFixture(t)
	ctx := context.Background()
	client, e := minio.New("127.0.0.1:"+os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT"), &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD"), ""), Transport: provisionTagFailureTransport{}})
	if e != nil {
		t.Fatal(e)
	}
	s.objects = client
	if e = s.createRestoreBucket(ctx, j); e != nil {
		t.Fatal(e)
	}
	tag, _ := tags.NewTags(map[string]string{"clawmanager-team-data-run": j.ID}, false)
	if e = s.objects.SetBucketTagging(ctx, teamDataBucket(j.ID), tag); e == nil {
		t.Fatal("tag injection failed")
	}
	finishFailedProvision(t, s, j)
	if e = s.cleanup(ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	if exists, e := s.objects.BucketExists(ctx, teamDataBucket(j.ID)); e != nil || exists {
		t.Fatal("bucket remains", e)
	}
}

func TestTeamDataProvisionIntentCannotDeleteForeignResources(t *testing.T) {
	for _, kind := range []string{"database", "bucket"} {
		t.Run(kind, func(t *testing.T) {
			s, j := teamDataProvisionFixture(t)
			ctx := context.Background()
			if kind == "database" {
				if _, e := s.admin.Exec("CREATE DATABASE `" + teamDataDatabase(j.ID) + "`"); e != nil {
					t.Fatal(e)
				}
				if _, e := s.prepareRestoreDatabase(ctx, j, func(context.Context, *sql.DB) error { return nil }); e == nil {
					t.Fatal("foreign db accepted")
				}
			} else {
				if e := s.objects.MakeBucket(ctx, teamDataBucket(j.ID), minio.MakeBucketOptions{}); e != nil {
					t.Fatal(e)
				}
				if e := s.createRestoreBucket(ctx, j); e == nil {
					t.Fatal("foreign bucket accepted")
				}
			}
			finishFailedProvision(t, s, j)
			if e := s.cleanup(ctx, j.ID); e == nil || e.Error() != "restore_ownership_unproven" {
				t.Fatal("intent authorized deletion", e)
			}
			if kind == "bucket" {
				if exists, e := s.objects.BucketExists(ctx, teamDataBucket(j.ID)); e != nil || !exists {
					t.Fatal("foreign bucket deleted", e)
				}
			} else {
				var n int
				if e := s.admin.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", teamDataDatabase(j.ID)).Scan(&n); e != nil || n != 1 {
					t.Fatal("foreign db deleted", e, n)
				}
			}
		})
	}
}

func TestTeamDataProvisionProcessInterruption(t *testing.T) {
	if os.Getenv("CM_PROVISION_CHILD") == "1" {
		cfg, _ := mysql.ParseDSN(os.Getenv("CM_PROVISION_ADMIN_DSN"))
		admin, e := sql.Open("mysql", cfg.FormatDSN())
		if e != nil {
			panic(e)
		}
		control, e := sql.Open("mysql", os.Getenv("CM_PROVISION_CONTROL_DSN"))
		if e != nil {
			panic(e)
		}
		s := &TeamDataService{db: control, admin: admin, adminConfig: cfg, owner: "test-worker", configHash: strings.Repeat("a", 64)}
		j := TeamDataJob{ID: os.Getenv("CM_PROVISION_JOB")}
		_, e = s.prepareRestoreDatabase(context.Background(), j, func(context.Context, *sql.DB) error { os.Exit(23); return nil })
		panic(e)
	}
	s, j := teamDataProvisionFixture(t)
	var controlName string
	if e := s.db.QueryRow("SELECT DATABASE()").Scan(&controlName); e != nil {
		t.Fatal(e)
	}
	cfg := s.adminConfig.Clone()
	cfg.DBName = controlName
	cmd := exec.Command(os.Args[0], "-test.run=^TestTeamDataProvisionProcessInterruption$")
	cmd.Env = append(os.Environ(), "CM_PROVISION_CHILD=1", "CM_PROVISION_ADMIN_DSN="+s.adminConfig.FormatDSN(), "CM_PROVISION_CONTROL_DSN="+cfg.FormatDSN(), "CM_PROVISION_JOB="+j.ID)
	output, e := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(e, &exited) || exited.ExitCode() != 23 {
		t.Fatalf("child did not exit at boundary: %v %s", e, output)
	}
	s.owner = "restarted-worker"
	ctx, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_ = s.runOwned(ctx)
	stop()
	actual, e := s.Get(context.Background(), j.ID)
	if e != nil || actual.Status != "failed" || actual.Phase != "interrupted" {
		t.Fatal("restart did not retain interruption", e, actual)
	}
	if e = s.cleanup(context.Background(), j.ID); e != nil {
		t.Fatal("interrupted preparation cleanup", e)
	}
}

func TestTeamDataUnknownCreationRequiresControlledResolution(t *testing.T) {
	for _, field := range []string{"state", "server"} {
		t.Run(field, func(t *testing.T) {
			s, j := teamDataProvisionFixture(t)
			ctx := context.Background()
			d, e := s.prepareRestoreDatabase(ctx, j, func(context.Context, *sql.DB) error { return nil })
			if e != nil {
				t.Fatal(e)
			}
			d.Close()
			value := "intent"
			if field == "server" {
				value = "different-server"
			}
			if _, e = s.db.Exec("UPDATE system_team_data_backup_jobs SET provisioning_json=JSON_SET(provisioning_json,?,?) WHERE public_id=?", "$.database."+field, value, j.ID); e != nil {
				t.Fatal(e)
			}
			finishFailedProvision(t, s, j)
			if e = s.cleanup(ctx, j.ID); e == nil || e.Error() != "restore_ownership_unproven" {
				t.Fatal("unknown result deleted", e)
			}
			// This fixture's parent has independent creation proof. Model an
			// operator removing that exact resource after review, never relabeling
			// an intent as a successful receipt to bypass the product's guard.
			if _, e = s.admin.Exec("DROP DATABASE `" + teamDataDatabase(j.ID) + "`"); e != nil {
				t.Fatal(e)
			}
			if e = s.cleanup(ctx, j.ID); e != nil {
				t.Fatal("cleanup after controlled resolution", e)
			}
		})
	}
}

func TestTeamDataSchemaAlignmentFailureCanBeCleaned(t *testing.T) {
	s, j := teamDataProvisionFixture(t)
	ctx := context.Background()
	// No mysql part: creation and trusted schema succeed, alignment then fails
	// before the old target-side ownership marker is written.
	if _, _, e := s.createRestoreSQL(ctx, j, PublishedArtifact{}); e == nil {
		t.Fatal("expected alignment failure")
	}
	if _, e := s.db.Exec("UPDATE system_team_data_backup_jobs SET status='failed' WHERE public_id=?", j.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.cleanup(ctx, j.ID); e != nil {
		t.Fatal("early failed owned database cannot be cleaned:", e)
	}
	var exists int
	if e := s.admin.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", teamDataDatabase(j.ID)).Scan(&exists); e != nil || exists != 0 {
		t.Fatal("database remains", e, exists)
	}
}
