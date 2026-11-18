package backupgate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestRealSQLGateDrainsTransactionsAndFencesEveryStatement(t *testing.T) {
	port := os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT")
	if port == "" {
		t.Skip("disposable MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + port, DBName: "mysql", AllowNativePasswords: true, ParseTime: true}
	admin, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Close() })
	var nonce [16]byte
	rand.Read(nonce[:])
	name := "cm_gate_" + hex.EncodeToString(nonce[:])
	if _, e = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec("DROP DATABASE `" + name + "`"); e != nil {
			t.Error(e)
		}
	})
	cfg.DBName = name
	raw, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { raw.Close() })
	for _, q := range []string{"CREATE TABLE system_backup_gate(scope VARCHAR(128) PRIMARY KEY,phase VARCHAR(32) NOT NULL) ENGINE=InnoDB", "INSERT INTO system_backup_gate VALUES('installation','idle')", "CREATE TABLE business(id INT PRIMARY KEY,value INT NOT NULL) ENGINE=InnoDB"} {
		if _, e = raw.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	base, e := mysql.NewConnector(&cfg)
	if e != nil {
		t.Fatal(e)
	}
	pool, e := Open(base, "installation")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Close() })
	pool.SetMaxOpenConns(4)
	if _, e = pool.ExecContext(ctx, "INSERT INTO business VALUES(?,?)", 1, 10); e != nil {
		t.Fatal(e)
	}
	prepared, e := pool.PrepareContext(ctx, "UPDATE business SET value=? WHERE id=?")
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Close()
	tx, e := pool.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(ctx, "UPDATE business SET value=11 WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	// The controller's exclusive gate lock cannot pass a live business tx.
	gateCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	_, e = raw.ExecContext(gateCtx, "UPDATE system_backup_gate SET phase='held' WHERE scope='installation'")
	stop()
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("gate passed a live writer", e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if _, e = raw.ExecContext(ctx, "UPDATE system_backup_gate SET phase='held' WHERE scope='installation'"); e != nil {
		t.Fatal(e)
	}
	for _, write := range []func() error{
		func() error { _, e := pool.ExecContext(ctx, "UPDATE business SET value=99 WHERE id=1"); return e },
		func() error { _, e := pool.ExecContext(ctx, "UPDATE business SET value=? WHERE id=?", 99, 1); return e },
		func() error { _, e := prepared.ExecContext(ctx, 99, 1); return e },
		func() error {
			tx, e := pool.BeginTx(ctx, nil)
			if tx != nil {
				tx.Rollback()
			}
			return e
		},
	} {
		if e = write(); !errors.Is(e, ErrMaintenance) {
			t.Fatal("statement escaped gate", e)
		}
	}
	var readValue int
	if e = pool.QueryRowContext(ctx, "SELECT value FROM business WHERE id=1").Scan(&readValue); e != nil || readValue != 11 {
		t.Fatal("maintenance blocked read-only status", readValue, e)
	}
	if r, e := pool.QueryContext(ctx, "UPDATE business SET value=99 WHERE id=1"); e == nil {
		r.Close()
		t.Fatal("query interface bypassed read-only protection")
	}
	if _, e = pool.ExecContext(ctx, "TRUNCATE TABLE business"); e == nil {
		t.Fatal("implicit-commit DDL escaped gate")
	}
	ro, e := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	var got int
	if e = ro.QueryRowContext(ctx, "SELECT value FROM business WHERE id=1").Scan(&got); e != nil || got != 11 {
		t.Fatal(got, e)
	}
	if _, e = ro.ExecContext(ctx, "UPDATE business SET value=99 WHERE id=1"); e == nil {
		t.Fatal("read-only bypass wrote data")
	}
	ro.Rollback()
	if _, e = raw.ExecContext(ctx, "UPDATE system_backup_gate SET phase='idle' WHERE scope='installation'"); e != nil {
		t.Fatal(e)
	}
	if _, e = prepared.ExecContext(ctx, 12, 1); e != nil {
		t.Fatal("resume did not release statements", e)
	}
	if e = pool.QueryRowContext(ctx, "SELECT value FROM business WHERE id=?", 1).Scan(&got); e != nil || got != 12 {
		t.Fatal(got, e)
	}
	if _, e = raw.ExecContext(ctx, "DELETE FROM system_backup_gate"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.ExecContext(ctx, "UPDATE business SET value=99 WHERE id=1"); !errors.Is(e, ErrMaintenance) {
		t.Fatal("missing gate failed open", e)
	}
}
