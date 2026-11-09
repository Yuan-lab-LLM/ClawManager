package systembackup

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestVerifierStreamsRowsWithBoundedDatabaseQueries(t *testing.T) {
	db := controlDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()
	if _, e := db.Exec("CREATE TABLE team_events (id INT PRIMARY KEY, payload VARBINARY(32)) ENGINE=InnoDB"); e != nil {
		t.Fatal(e)
	}
	values := make([]string, 1000)
	for i := range values {
		values[i] = "(?,?)"
	}
	args := make([]any, 0, 2000)
	for i := range values {
		args = append(args, i, []byte{0, 255, byte(i)})
	}
	if _, e := db.Exec("INSERT INTO team_events VALUES "+strings.Join(values, ","), args...); e != nil {
		t.Fatal(e)
	}
	var snapshot bytes.Buffer
	if _, e := ExportMySQL(ctx, db, &snapshot, []string{"team_events"}); e != nil {
		t.Fatal(e)
	}
	questions := func() int64 {
		var name string
		var value int64
		if e := db.QueryRow("SHOW SESSION STATUS LIKE 'Questions'").Scan(&name, &value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	before := questions()
	if _, e := VerifyMySQL(ctx, db, bytes.NewReader(snapshot.Bytes()), 1000); e != nil {
		t.Fatal(e)
	}
	if count := questions() - before; count > 20 {
		t.Fatalf("verifier used %d SQL round trips for one table; expected bounded streaming", count)
	}
}

func TestSQLTimeCellPreservesDeclaredPrecision(t *testing.T) {
	at := time.Date(2026, 10, 23, 1, 2, 3, 123400000, time.UTC)
	for _, tc := range []struct{ kind, want string }{{"datetime(6)", "2026-10-23 01:02:03.123400"}, {"timestamp(3)", "2026-10-23 01:02:03.123"}, {"datetime", "2026-10-23 01:02:03"}, {"date", "2026-10-23"}} {
		c, e := sqlCell(at, SQLColumn{Type: tc.kind})
		if e != nil || string(c.Data) != tc.want {
			t.Fatalf("%s: %s %v", tc.kind, c.Data, e)
		}
	}
}

func TestReadOnlyVerifierDetectsValueLossExtraRowsAndBudget(t *testing.T) {
	db := controlDB(t)
	ctx := context.Background()
	_, err := db.Exec(`CREATE TABLE team_events (id BIGINT PRIMARY KEY, payload LONGBLOB NULL, created_at DATETIME(6) NOT NULL) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO team_events VALUES(1,?,'2026-10-23 01:02:03.123400')", []byte{0, 255, 10})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err = ExportMySQL(ctx, db, &archive, []string{"team_events"}); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMySQL(ctx, db, bytes.NewReader(archive.Bytes()), 10); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE team_events SET payload='lost' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMySQL(ctx, db, bytes.NewReader(archive.Bytes()), 10); err == nil {
		t.Fatal("missed corruption")
	}
	if _, err = db.Exec("UPDATE team_events SET payload=? WHERE id=1", []byte{0, 255, 10}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO team_events VALUES(2,NULL,'2026-10-23 01:02:03.123400')"); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMySQL(ctx, db, bytes.NewReader(archive.Bytes()), 10); err == nil {
		t.Fatal("missed additional row")
	}
	archive.Reset()
	if _, err = ExportMySQL(ctx, db, &archive, []string{"team_events"}); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMySQL(ctx, db, bytes.NewReader(archive.Bytes()), 1); err == nil {
		t.Fatal("ignored budget")
	}
}
