package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
)

func runtimeProtocolMySQLFixture(t *testing.T) (*runtimeProtocolRepository, func(string, ...any)) {
	t.Helper()
	dsn := os.Getenv("CLAWMANAGER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLAWMANAGER_TEST_MYSQL_DSN not configured")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("requires a disposable clawmanager_test_ schema")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true", "charset": "utf8mb4"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("refusing non-empty database")
	}
	t.Cleanup(func() {
		for _, table := range []string{"gateway_runtime_protocols", "instances", "users"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE users (id INT PRIMARY KEY,is_active BOOLEAN NOT NULL) ENGINE=InnoDB")
	exec("CREATE TABLE instances (id INT PRIMARY KEY,user_id INT NOT NULL,runtime_generation INT NOT NULL) ENGINE=InnoDB")
	exec("INSERT INTO users VALUES (1,TRUE),(2,TRUE)")
	exec("INSERT INTO instances VALUES (10,1,3)")
	raw, err := os.ReadFile("../db/migrations/064_add_runtime_protocol.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	return &runtimeProtocolRepository{sess: sess}, exec
}

func TestRuntimeCapabilityRenewalMySQL(t *testing.T) {
	r, exec := runtimeProtocolMySQLFixture(t)
	report := models.RuntimeCapabilityReport{Protocol: 1, Generation: 3, RuntimeVersion: "native.1", AdapterVersion: "adapter.1", Capabilities: models.RequiredRuntimeCapabilities()}
	get := func() *models.RuntimeProtocolState {
		t.Helper()
		s, err := r.Get(t.Context(), 10)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if err := r.Renew(t.Context(), 10, 1, report); !errors.Is(err, models.ErrRuntimeCapabilityRenewalConflict) {
		t.Fatalf("legacy enrollment: %v", err)
	}
	if get() != nil {
		t.Fatal("renew created enrollment")
	}
	if err := r.Negotiate(t.Context(), 10, 1, report); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE gateway_runtime_protocols SET reported_at='2020-01-01 00:00:00' WHERE instance_id=10")
	before := get()
	reordered := report
	reordered.Capabilities = []string{"live_catalog", "cancel_receipt", "turn_cancel", "task_identity"}
	if err := r.Renew(t.Context(), 10, 1, reordered); err != nil {
		t.Fatal(err)
	}
	after := get()
	if !after.ReportedAt.After(before.ReportedAt) {
		t.Fatal("renew did not advance freshness")
	}
	after.ReportedAt = before.ReportedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatal("renew changed the enrolled contract")
	}
	if err := r.Renew(t.Context(), 10, 1, report); err != nil {
		t.Fatalf("repeat renewal: %v", err)
	}
	for _, kind := range []string{"version", "capabilities", "old_generation", "other_owner", "disabled_user", "corrupt_report", "future_protocol"} {
		t.Run(kind, func(t *testing.T) {
			request, owner := report, 1
			want := models.ErrRuntimeCapabilityRenewalConflict
			switch kind {
			case "version":
				request.AdapterVersion = "adapter.2"
			case "capabilities":
				request.Capabilities = append(append([]string{}, report.Capabilities...), "maintenance_drain")
			case "old_generation":
				request.Generation = 2
				want = models.ErrModelUnavailable
			case "other_owner":
				owner = 2
				want = models.ErrModelUnavailable
			case "disabled_user":
				exec("UPDATE users SET is_active=FALSE WHERE id=1")
				want = models.ErrModelUnavailable
			case "corrupt_report":
				exec("UPDATE gateway_runtime_protocols SET capabilities_json='invalid' WHERE instance_id=10")
			case "future_protocol":
				exec("UPDATE gateway_runtime_protocols SET protocol=2 WHERE instance_id=10")
			}
			old := get()
			if err := r.Renew(t.Context(), 10, owner, request); !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
			if !reflect.DeepEqual(old, get()) {
				t.Fatal("failed renewal mutated report")
			}
			exec("UPDATE users SET is_active=TRUE WHERE id=1")
			exec("UPDATE gateway_runtime_protocols SET protocol=1,capabilities_json=? WHERE instance_id=10", before.CapabilitiesJSON)
		})
	}
	// Hold an instance row lock while a renewal is pending, then change the
	// generation. The old report must not refresh after that transaction commits.
	database, err := sql.Open("mysql", os.Getenv("CLAWMANAGER_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, "SELECT runtime_generation FROM instances WHERE id=10 FOR UPDATE").Scan(&current); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- r.Renew(ctx, 10, 1, report) }()
	select {
	case err := <-finished:
		t.Fatalf("renew ignored instance lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	old := get()
	if _, err := tx.ExecContext(ctx, "UPDATE instances SET runtime_generation=4 WHERE id=10"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("stale concurrent renewal: %v", err)
	}
	if !reflect.DeepEqual(old, get()) {
		t.Fatal("restart race refreshed old enrollment")
	}
	report.Generation = 4
	if err := r.Renew(t.Context(), 10, 1, report); !errors.Is(err, models.ErrRuntimeCapabilityRenewalConflict) {
		t.Fatalf("renew enrolled new generation: %v", err)
	}
	if err := r.Negotiate(t.Context(), 10, 1, report); err != nil {
		t.Fatal(err)
	}
	if err := r.Renew(t.Context(), 10, 1, report); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE instances SET user_id=2 WHERE id=10")
	if err := r.Renew(t.Context(), 10, 1, report); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("previous owner renewed: %v", err)
	}
	if err := r.Renew(t.Context(), 10, 2, report); !errors.Is(err, models.ErrRuntimeCapabilityRenewalConflict) {
		t.Fatalf("new owner silently took over report: %v", err)
	}
}
