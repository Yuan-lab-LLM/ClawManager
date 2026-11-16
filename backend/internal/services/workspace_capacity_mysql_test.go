package services

import (
	"clawreef/internal/models"
	"context"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceCapacityUsesNativeProspectiveUpgradePlacementMySQL(t *testing.T) {
	dsn := os.Getenv("WORKSPACE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("empty isolated MySQL required")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("refusing non-test DB")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty DB")
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, table := range []string{"runtime_upgrade_items", "runtime_rollouts", "instances"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	}()
	exec(`CREATE TABLE instances(id INT PRIMARY KEY,created_at DATETIME)`)
	exec(`CREATE TABLE runtime_upgrade_items(id INT PRIMARY KEY,rollout_id INT,instance_id INT)`)
	exec(`CREATE TABLE runtime_rollouts(id BIGINT PRIMARY KEY,runtime_type VARCHAR(32),status VARCHAR(32),source_images_json JSON,target_image_ref VARCHAR(255),target_image_digest VARCHAR(255),phase VARCHAR(64),started_at DATETIME,preflight_id VARCHAR(64),preflight_json JSON,plan_fingerprint VARCHAR(255))`)
	target := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
	started := time.Now().Add(-time.Hour).UTC()
	exec(`INSERT INTO runtime_rollouts VALUES(1,'openclaw','finished','{"tenant/pool":"registry/old"}',?,?,'empty_pool_reset',?,NULL,NULL,'')`, target, "sha256:"+strings.Repeat("b", 64), started)
	service := &RuntimeUpgradeService{sess: sess}
	prospective := time.Now().UTC()
	pods := []models.RuntimePod{{ID: 1, Namespace: "tenant", DeploymentName: "pool", ImageRef: target}, {ID: 2, Namespace: "tenant", DeploymentName: "unrelated", ImageRef: target}, {ID: 3, Namespace: "tenant", DeploymentName: "pool", ImageRef: "registry/old"}}
	eligible, err := service.filterUpgradePlacementAtBirth(context.Background(), 0, pods, nil, &prospective)
	if err != nil || len(eligible) != 1 || eligible[0].ID != 1 {
		t.Fatal("native default scope lost", eligible, err)
	}
	exec("INSERT INTO instances VALUES(99,?)", started.Add(-time.Hour))
	if _, err = service.filterUpgradePlacement(context.Background(), 99, pods); err == nil {
		t.Fatal("existing pre-reset birth fence changed")
	}
	if _, err = service.filterUpgradePlacementAtBirth(context.Background(), 99, pods, nil, &prospective); err == nil {
		t.Fatal("prospective birth overrode an existing instance")
	}
	exec("UPDATE instances SET created_at=? WHERE id=99", started.Add(time.Minute))
	existing, existingErr := service.filterUpgradePlacement(context.Background(), 99, pods)
	if existingErr != nil || len(existing) != 1 || existing[0].ID != 1 {
		t.Fatal("existing post-reset placement changed", existing, existingErr)
	}
	exec("DELETE FROM instances WHERE id=99")
	row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM instances")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal("capacity created an instance", err, count)
	}
}
