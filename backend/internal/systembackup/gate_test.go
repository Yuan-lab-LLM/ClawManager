package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func fixtureDatabase(t *testing.T) *sql.DB {
	t.Helper()
	port := os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT")
	if port == "" {
		t.Skip("requires disposable localhost MySQL")
	}
	c := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + port, DBName: "mysql", ParseTime: true, AllowNativePasswords: true}
	root, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	name, err := RandomID("cm_control_")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = root.Exec("CREATE DATABASE `" + name + "`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := root.Exec("DROP DATABASE `" + name + "`"); e != nil {
			t.Error(e)
		}
	})
	c.DBName = name
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func controlDB(t *testing.T) *sql.DB {
	t.Helper()
	db := fixtureDatabase(t)
	for _, file := range []string{"079_add_system_backup_control.sql", "080_add_backup_dependency_health.sql", "081_add_backup_controller_coordination.sql", "082_add_backup_attempt_request.sql", "083_add_backup_strong_auth_nonce.sql", "084_add_backup_external_actions.sql", "085_add_backup_capture_coordination.sql", "086_add_backup_capture_abort.sql", "087_add_backup_stage_resources.sql", "088_add_backup_core_publication.sql", "089_add_backup_gate_admission.sql", "090_add_backup_restore_job_executions.sql", "091_add_backup_job_send_intents.sql", "092_allow_owned_backup_restore_workspaces.sql"} {
		directory := os.Getenv("TEST_SYSTEM_BACKUP_MIGRATIONS_DIR")
		if directory == "" {
			directory = "../db/migrations"
		}
		ddl, err := os.ReadFile(directory + "/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(ddl), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err = db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return db
}

func TestGateAcceptsIndependentConfiguredAcquireAndHoldBudgets(t *testing.T) {
	db := controlDB(t)
	g := Gate{db}
	ctx := context.Background()
	for i, budget := range [][2]time.Duration{{60 * time.Second, 30 * time.Second}, {10 * time.Second, time.Hour}} {
		scope := "budget-" + string(rune('a'+i))
		if err := g.EnsureScope(ctx, scope); err != nil {
			t.Fatal(err)
		}
		own, err := g.Begin(ctx, scope, "backup-budget", RegistryHash(), budget[0], budget[1])
		if err != nil {
			t.Fatalf("valid backup configuration rejected: %v", err)
		}
		if _, err = g.Hold(ctx, own); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGateRequiresExpiredWritersToStopAndAllParticipantsToAck(t *testing.T) {
	db := controlDB(t)
	g := Gate{db}
	ctx := context.Background()
	if err := g.EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterParticipant(ctx, "installation", "runtime-1"); err != nil {
		t.Fatal(err)
	}
	lease, err := g.AcquireMutation(ctx, "installation", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	own, err := g.Begin(ctx, "installation", "backup-1", RegistryHash(), time.Minute, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.AcquireMutation(ctx, "installation", time.Minute); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("new mutation admitted: %v", err)
	}
	if _, err = db.Exec("UPDATE system_backup_mutation_leases SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE public_id=?", lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Hold(ctx, own); !errors.Is(err, ErrMaintenance) {
		t.Fatal("expired lease mistaken for stopped writer", err)
	}
	if err = g.ReleaseMutation(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Hold(ctx, own); !errors.Is(err, ErrMaintenance) {
		t.Fatal("missing runtime ACK ignored", err)
	}
	stale := own
	stale.Generation--
	if err = g.AckPaused(ctx, stale, "runtime-1", RegistryHash()); !errors.Is(err, ErrFence) {
		t.Fatal("old ACK accepted", err)
	}
	if err = g.AckPaused(ctx, own, "runtime-1", RegistryHash()); err != nil {
		t.Fatal(err)
	}
	own, err = g.Hold(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.ValidateCapture(ctx, own); err != nil {
		t.Fatal(err)
	}
	if err = g.BeginRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	if err = g.FinishRelease(ctx, own); !errors.Is(err, ErrMaintenance) {
		t.Fatal("released before runtime resumed", err)
	}
	if err = g.AckResumed(ctx, own, "runtime-1", false); err != nil {
		t.Fatal(err)
	}
	if err = g.FinishRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	if err = g.ValidateCapture(ctx, own); !errors.Is(err, ErrFence) {
		t.Fatal("old capture token still valid", err)
	}
}

func TestGateBusinessWriteRollsBackWhenLeaseExpires(t *testing.T) {
	db := controlDB(t)
	g := Gate{db}
	ctx := context.Background()
	_ = g.EnsureScope(ctx, "installation")
	if _, err := db.Exec("CREATE TABLE gate_fixture (id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	lease, err := g.AcquireMutation(ctx, "installation", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = g.WriteWithLease(ctx, lease, func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO gate_fixture VALUES (1)"); e != nil {
			return e
		}
		_, e := tx.Exec("UPDATE system_backup_mutation_leases SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE public_id=?", lease.ID)
		return e
	})
	if !errors.Is(err, ErrFence) {
		t.Fatal("lease expiry not detected", err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM gate_fixture").Scan(&count); err != nil || count != 0 {
		t.Fatal("business write committed without lease", err)
	}
}
