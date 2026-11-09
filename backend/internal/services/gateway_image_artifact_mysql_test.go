package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

// Opt-in only. Use a dedicated EMPTY disposable database, never the application
// database or the runtime-governance test database (packages run concurrently).
func TestImageArtifactMySQLOwnershipAndRetention(t *testing.T) {
	sess := imageMySQLTestSession(t)
	schema, err := os.ReadFile("../db/migrations/072_add_gateway_image_artifacts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = sess.SQL().Exec("DROP TABLE IF EXISTS gateway_image_artifacts") })
	t.Run("local", func(t *testing.T) {
		testImageArtifactStorage(t, sess, &objectStorageService{localPath: t.TempDir(), basePath: "skills"})
	})
	t.Run("minio", func(t *testing.T) {
		endpoint := os.Getenv("CLAWMANAGER_IMAGE_TEST_S3_ENDPOINT")
		if endpoint == "" {
			t.Skip("requires optional disposable local MinIO endpoint")
		}
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			t.Fatal("refusing non-loopback object storage test endpoint")
		}
		storage, err := NewObjectStorageService(config.ObjectStorageConfig{Endpoint: endpoint, AccessKey: os.Getenv("CLAWMANAGER_IMAGE_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("CLAWMANAGER_IMAGE_TEST_S3_SECRET_KEY"), Bucket: fmt.Sprintf("clawmanager-test-images-%d", time.Now().UnixNano()), BasePath: "skills"})
		if err != nil {
			t.Fatal(err)
		}
		objects := storage.(*objectStorageService)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := objects.minioClient.RemoveBucket(ctx, objects.bucket); err != nil {
				t.Error("temporary image bucket cleanup failed", err)
			}
		})
		testImageArtifactStorage(t, sess, objects)
	})
}

func imageMySQLTestSession(t *testing.T) db.Session {
	t.Helper()
	dsn := os.Getenv("CLAWMANAGER_IMAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires CLAWMANAGER_IMAGE_TEST_MYSQL_DSN for a disposable empty database")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_images_") {
		t.Fatal("refusing invalid or non-image-test database")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal("unable to connect to disposable image test database")
	}
	t.Cleanup(func() { _ = sess.Close() })
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal("refusing non-empty test database")
	}
	return sess
}

func testImageArtifactStorage(t *testing.T, sess db.Session, storage *objectStorageService) {
	t.Helper()
	svc, err := NewGatewayImageArtifactService(sess, storage)
	if err != nil {
		t.Fatal(err)
	}
	instanceID, keyID, other := 8, 9, 10
	owner := ImageArtifactOwner{UserID: 7, InstanceID: &instanceID, GatewayKeyID: &keyID}
	plain := []byte("private generated image test bytes")
	id, err := svc.Save(t.Context(), owner, 3, "request-test", "image/png", plain)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := storage.GetObjectLimited(t.Context(), "gateway-images/"+id, maxStoredImageArtifactBytes)
	if err != nil || bytes.Contains(stored, plain) {
		t.Fatal("image stored unencrypted", err)
	}
	for _, allowed := range []ImageArtifactOwner{owner, {UserID: 7}, {UserID: 7, InstanceID: &instanceID}, {UserID: 7, GatewayKeyID: &keyID}} {
		ids, err := svc.ListForRequest(t.Context(), allowed, "request-test")
		if err != nil || len(ids) != 1 || ids[0] != id {
			t.Fatal("saved artifact not recoverable", ids, err)
		}
		got, err := svc.Load(t.Context(), allowed, id)
		if err != nil || !bytes.Equal(got.Data, plain) || got.ModelID != 3 {
			t.Fatal("authorized image read failed", err)
		}
	}
	for _, denied := range []ImageArtifactOwner{{UserID: 0}, {UserID: 6}, {UserID: 7, InstanceID: &other}, {UserID: 7, GatewayKeyID: &other}} {
		ids, _ := svc.ListForRequest(t.Context(), denied, "request-test")
		if len(ids) != 0 {
			t.Fatal("cross-owner artifact recovery allowed", ids)
		}
		if _, err := svc.Load(t.Context(), denied, id); !errors.Is(err, ErrImageArtifactUnavailable) {
			t.Fatal("cross-owner/credential image read allowed", denied, err)
		}
	}
	if _, err = sess.SQL().Exec("UPDATE gateway_image_artifacts SET ready=FALSE WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Load(t.Context(), owner, id); !errors.Is(err, ErrImageArtifactUnavailable) {
		t.Fatal("pending image exposed", err)
	}
	if ids, err := svc.ListForRequest(t.Context(), owner, "request-test"); err != nil || len(ids) != 0 {
		t.Fatal("pending artifact recovery exposed", ids, err)
	}
	if _, err = sess.SQL().Exec("UPDATE gateway_image_artifacts SET ready=TRUE,expires_at='2000-01-01' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Load(t.Context(), owner, id); !errors.Is(err, ErrImageArtifactUnavailable) {
		t.Fatal("expired image exposed", err)
	}
	if ids, err := svc.ListForRequest(t.Context(), owner, "request-test"); err != nil || len(ids) != 0 {
		t.Fatal("expired artifact recovery exposed", ids, err)
	}
	if err = svc.PurgeExpired(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.GetObjectLimited(t.Context(), "gateway-images/"+id, maxStoredImageArtifactBytes); err == nil {
		t.Fatal("expired object not deleted")
	}
	if err = svc.PurgeExpired(t.Context()); err != nil {
		t.Fatal("cleanup not idempotent", err)
	}
}
