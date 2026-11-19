package systembackup

import (
	"bytes"
	"context"
	"testing"
	"time"

	"clawreef/internal/historymaintenance"
)

func TestTeamDataHistoryScopeIncludesOnlyTeamMonthlyAndMetadata(t *testing.T) {
	ctx := context.Background()
	db := fixtureDatabase(t)
	historyDDL(t, db)
	svc := historymaintenance.New(db, nil, nil)
	if err := svc.PrepareMonths(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"CREATE TABLE observation_invocation_summaries(id BIGINT PRIMARY KEY,payload TEXT) ENGINE=InnoDB",
		"CREATE TABLE observability_admissions(id BIGINT PRIMARY KEY,payload TEXT) ENGINE=InnoDB",
		"INSERT INTO observation_invocation_summaries VALUES(1,'unrelated AI metadata')",
		"INSERT INTO observability_admissions VALUES(1,'unrelated AI identity')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	names, err := snapshotBusinessTables(ctx, db, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if k, _, ok := historymaintenance.ParseMonthly(n); ok && k != "team_events" && k != "team_tasks" {
			t.Fatalf("Team inventory includes unrelated month %s", n)
		}
	}
	var out bytes.Buffer
	result, err := ExportTeamDataMySQL(ctx, db, &out, append(names, "model_invocations_202611"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte("unrelated AI")) {
		t.Fatal("Team snapshot leaked AI metadata")
	}
	if result.Tables["history_month_catalog"] != 6 {
		t.Fatalf("Team catalog includes unrelated kinds: %v", result.Tables)
	}
	if _, ok := result.Tables["model_invocations_202611"]; ok {
		t.Fatal("non-Team monthly table in frozen SQL inventory")
	}
	if len(result.MonthlyCatalog) != 6 {
		t.Fatalf("monthly inventory: %d", len(result.MonthlyCatalog))
	}
	if _, err = validateTeamDataSQLSnapshot(bytes.NewReader(out.Bytes()), CaptureBudget{MaxItems: 100}); err == nil {
		t.Fatal("minimal noncanonical static schema accepted")
	}
}

func TestTeamDataMonthlyNativeSchemaRestore(t *testing.T) {
	ctx := context.Background()
	source := fullSchemaDB(t)
	if err := historymaintenance.New(source, nil, nil).PrepareMonths(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTeamDataSchema(ctx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(`INSERT INTO team_events_202611(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes) VALUES(17,12,'2026-11-02','trace',3,'{}',REPEAT('a',64),2)`); err != nil {
		t.Fatal(err)
	}
	names, err := snapshotBusinessTables(ctx, source, true)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if _, err = ExportTeamDataMySQL(ctx, source, &snapshot, names); err != nil {
		t.Fatal(err)
	}
	if _, err = validateTeamDataSQLSnapshot(bytes.NewReader(snapshot.Bytes()), CaptureBudget{MaxItems: 10000}); err != nil {
		t.Fatal(err)
	}
	target, name := isolatedSchemaDB(t, source)
	if err = alignTeamDataRestoreSchema(ctx, target, bytes.NewReader(snapshot.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err = RestoreMySQL(ctx, target, "source", name, bytes.NewReader(snapshot.Bytes()), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMySQL(ctx, target, bytes.NewReader(snapshot.Bytes()), 10000); err != nil {
		t.Fatal(err)
	}
	var owner int
	if err = target.QueryRow(`SELECT owner_id FROM team_events_202611 WHERE original_id=17`).Scan(&owner); err != nil || owner != 12 {
		t.Fatalf("stable public ID/owner lost: %d %v", owner, err)
	}
}
