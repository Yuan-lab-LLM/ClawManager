package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/repository"
	"github.com/go-sql-driver/mysql"
	upper "github.com/upper/db/v4"
)

func TestGateAcquisitionDoesNotQueueAheadOfAnAdmittedRequestsSQL(t *testing.T) {
	ctx := context.Background()
	raw := controlDB(t)
	g := Gate{raw}
	if err := g.EnsureScope(ctx, "nested-request"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE fixture_writes(id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	var database string
	if err := raw.QueryRow("SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	cfg := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: database, AllowNativePasswords: true, ParseTime: true}
	connector, err := mysql.NewConnector(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	app, err := backupgate.Open(connector, "nested-request")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	release, err := backupgate.HoldRequest(ctx, raw, "nested-request")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	acquire, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := g.Begin(acquire, "nested-request", "capture", RegistryHash(), time.Second, 5*time.Second)
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrMaintenance) {
			t.Fatal("busy gate must retry without queuing", err)
		}
	case <-time.After(75 * time.Millisecond):
	}
	write, stop := context.WithTimeout(ctx, 400*time.Millisecond)
	defer stop()
	if _, err = app.ExecContext(write, "INSERT INTO fixture_writes VALUES(1)"); err != nil {
		t.Fatal("an already admitted request deadlocked behind gate acquisition", err)
	}
	release()
}

func TestRequestBoundaryDrainsExternalWritesDespiteClientCancellation(t *testing.T) {
	ctx := context.Background()
	raw := controlDB(t)
	gate := Gate{DB: raw}
	if e := gate.EnsureScope(ctx, "requests"); e != nil {
		t.Fatal(e)
	}
	requestCtx, cancel := context.WithCancel(ctx)
	release, e := backupgate.HoldRequest(requestCtx, raw, "requests")
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	cancel() // The handler can still be writing despite its client disconnecting.
	wait, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	if _, e = gate.Begin(wait, "requests", "capture", RegistryHash(), time.Second, 5*time.Second); e == nil {
		stop()
		t.Fatal("gate passed an unfinished HTTP side effect")
	}
	stop()
	file := filepath.Join(t.TempDir(), "partial-result")
	if e = os.WriteFile(file, []byte("complete handler write"), 0600); e != nil {
		t.Fatal(e)
	}
	release()
	owner, e := gate.Begin(ctx, "requests", "capture", RegistryHash(), time.Second, 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = backupgate.HoldRequest(ctx, raw, "requests"); !errors.Is(e, backupgate.ErrMaintenance) {
		t.Fatal("new handler crossed maintenance", e)
	}
	if e = gate.BeginRelease(ctx, owner); e != nil {
		t.Fatal(e)
	}
	if e = gate.FinishRelease(ctx, owner); e != nil {
		t.Fatal(e)
	}
	if e = backupgate.RequireSameDatabase(ctx, raw, raw); e != nil {
		t.Fatal(e)
	}
	if e = backupgate.RequireSameDatabase(ctx, raw, fixtureDatabase(t)); e == nil {
		t.Fatal("different physical gate database accepted")
	}
}

func TestDisconnectedBoundaryConnectionDoesNotProveExternalWriterStopped(t *testing.T) {
	ctx := context.Background()
	admin := controlDB(t)
	g := Gate{admin}
	if err := g.EnsureScope(ctx, "lost-boundary"); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := admin.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	cfg := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: name, AllowNativePasswords: true, ParseTime: true}
	writer, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	var connection uint64
	if err = writer.QueryRow("SELECT CONNECTION_ID()").Scan(&connection); err != nil {
		t.Fatal(err)
	}
	release, err := backupgate.HoldRequest(ctx, writer, "lost-boundary")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = admin.Exec("KILL CONNECTION " + strconv.FormatUint(connection, 10)); err != nil {
		t.Fatal(err)
	}
	own, err := g.Begin(ctx, "lost-boundary", "backup", RegistryHash(), 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatal("dead connection retained its physical lock", err)
	}
	if _, err = g.Hold(ctx, own); !errors.Is(err, ErrMaintenance) {
		t.Fatal("SQL disconnection falsely certified an unfinished file writer", err)
	}
	release()
	own, err = g.Hold(ctx, own)
	if err != nil {
		t.Fatal("completed writer did not release its durable blocker", err)
	}
	if err = g.BeginRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	if err = g.FinishRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
}

func TestAcquisitionClosesNewRequestAdmissionWhileExistingWriterDrains(t *testing.T) {
	ctx := context.Background()
	raw := controlDB(t)
	g := Gate{raw}
	if err := g.EnsureScope(ctx, "close-admission"); err != nil {
		t.Fatal(err)
	}
	release, err := backupgate.HoldRequest(ctx, raw, "close-admission")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = g.Begin(ctx, "close-admission", "backup", RegistryHash(), 5*time.Second, 30*time.Second); !errors.Is(err, ErrMaintenance) {
		t.Fatal("existing writer was not drained", err)
	}
	newRelease, err := backupgate.HoldRequest(ctx, raw, "close-admission")
	if err == nil {
		newRelease()
		t.Fatal("new requests can starve backup acquisition indefinitely")
	}
	if !errors.Is(err, backupgate.ErrMaintenance) {
		t.Fatal(err)
	}
	release()
	own, err := g.Begin(ctx, "close-admission", "backup", RegistryHash(), 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	own, err = g.Hold(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.BeginRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	if err = g.FinishRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	newRelease, err = backupgate.HoldRequest(ctx, raw, "close-admission")
	if err != nil {
		t.Fatal("normal admission was not restored", err)
	}
	newRelease()
}

func TestApplicationSQLGateCoversUpperRepositories(t *testing.T) {
	ctx := context.Background()
	raw := fullSchemaDB(t)
	var name string
	if e := raw.QueryRow("SELECT DATABASE()").Scan(&name); e != nil {
		t.Fatal(e)
	}
	gate := Gate{DB: raw}
	if e := gate.EnsureScope(ctx, "application"); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SYSTEM_BACKUP_SQL_GATE_SCOPE", "application")
	port, _ := strconv.Atoi(os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"))
	cfg := config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name, MaxOpenConns: 4, MaxIdleConns: 2}
	session, e := appdb.Initialize(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	if _, e = session.SQL().Exec("INSERT INTO teams(id,user_id,name,status) VALUES(991,1,'gate-fixture','stopped')"); e != nil {
		t.Fatal(e)
	}
	repo := repository.NewTeamRepository(session)
	if _, e = repo.GetTeamByID(991); e != nil {
		t.Fatal(e)
	}
	if e = session.Collection("teams").Find(upper.Cond{"id": 991}).Update(map[string]any{"name": "before-gate"}); e != nil {
		t.Fatal(e)
	}
	owner, e := gate.Begin(ctx, "application", "test-controller", RegistryHash(), time.Second, 5*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	owner, e = gate.Hold(ctx, owner)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = repo.GetTeamByID(991); e != nil {
		t.Fatal("repository reads must remain available", e)
	}
	if e = session.Collection("teams").Find(upper.Cond{"id": 991}).Update(map[string]any{"name": "escaped"}); !errors.Is(e, backupgate.ErrMaintenance) {
		t.Fatal("ORM update escaped gate", e)
	}
	if _, e = appdb.Initialize(cfg); !errors.Is(e, backupgate.ErrMaintenance) {
		t.Fatal("business startup escaped maintenance", e)
	}
	if e = gate.BeginRelease(ctx, owner); e != nil {
		t.Fatal(e)
	}
	if e = gate.FinishRelease(ctx, owner); e != nil {
		t.Fatal(e)
	}
	if e = session.Collection("teams").Find(upper.Cond{"id": 991}).Update(map[string]any{"name": "after-gate"}); e != nil {
		t.Fatal(e)
	}
}
