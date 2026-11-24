package records

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestTrialScopeKeepsOnlySelectedOwners(t *testing.T) {
	selection := TrialScope{Instances: []TrialInstanceChoice{{ID: 7, UserID: 1}}}
	if !selection.Valid() {
		t.Fatal("an instance selection can include its owner implicitly")
	}
	snapshot := trialFixture()
	for _, table := range snapshot.Tables {
		for _, row := range table.Rows {
			switch table.Name {
			case "instance_config_revisions":
				row["published_by"] = Text("3")
			case "instance_desired_state":
				row["updated_by"] = Text("1")
			}
		}
	}
	if err := matchTrialScope(snapshot, selection); err != nil {
		t.Fatal(err)
	}
	for _, table := range snapshot.Tables {
		switch table.Name {
		case "instance_config_revisions":
			if !table.Rows[0]["published_by"].Null {
				t.Fatal("revision still refers to an excluded account")
			}
		case "instance_desired_state":
			if table.Rows[0]["updated_by"].String() != "1" {
				t.Fatal("selected actor was removed")
			}
		}
	}
	selection.Instances[0].UserID = 2
	if err := matchTrialScope(snapshot, selection); !errors.Is(err, ErrTrialScope) {
		t.Fatalf("changed owner was accepted: %v", err)
	}
}

// The fixture uses separate disposable MySQL servers with the application
// migration catalog and a source user granted SELECT on TrialSourceTables.
func TestTrialScopeMySQLRoundTrip(t *testing.T) {
	if os.Getenv("CLAWMANAGER_TRIAL_SCOPE_MYSQL_TEST") != "1" {
		t.Skip("requires disposable scope fixture databases")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	open := func(key string) *sql.DB {
		t.Helper()
		pool, err := sql.Open("mysql", os.Getenv(key))
		if err != nil {
			t.Fatalf("invalid fixture connection for %s", key)
		}
		t.Cleanup(func() { pool.Close() })
		return pool
	}
	const sourceName = "cm_drill_trial_scope_source"
	const targetName = "cm_drill_trial_scope_target"
	source := open("TRIAL_SCOPE_MYSQL_SOURCE_DSN")
	sourceUUID := os.Getenv("TRIAL_SCOPE_MYSQL_SOURCE_UUID")
	options, err := ListTrialOptions(ctx, source, sourceName, sourceUUID, 0, 202)
	if err != nil || len(options.Users) != 3 || !options.Users[0].Admin || options.Users[2].Admin ||
		len(options.Instances) != 2 || options.Instances[0].ID != 203 || options.Instances[0].UserID != 20 {
		t.Fatalf("unexpected selection options: %+v, %v", options, err)
	}
	limits := Limits{MaxRows: 1000, MaxBytes: 1 << 20}
	choice := TrialScope{UserIDs: []int64{1}, Instances: []TrialInstanceChoice{{ID: 202, UserID: 20}}}
	snapshot, err := CaptureSelectedTrial(ctx, source, limits, sourceName, sourceUUID, choice)
	if err != nil {
		t.Fatal(err)
	}
	rows := make(map[string][]Row)
	for _, table := range snapshot.Tables {
		rows[table.Name] = table.Rows
	}
	for table, want := range map[string][]string{
		"users": {"1", "20"}, "instances": {"202"}, "user_quotas": {"1", "20"},
		"openclaw_injection_snapshots": {"62"},
		"instance_desired_state":       {"52"}, "instance_config_revisions": {"42"},
	} {
		var ids []string
		for _, row := range rows[table] {
			ids = append(ids, row["id"].String())
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("%s captured IDs = %v, want %v", table, ids, want)
		}
	}
	if !rows["instance_desired_state"][0]["updated_by"].Null || !rows["instance_config_revisions"][0]["published_by"].Null {
		t.Fatal("captured startup rows refer to the excluded actor")
	}
	for _, invalid := range []TrialScope{
		{UserIDs: []int64{1}, Instances: []TrialInstanceChoice{{ID: 202, UserID: 30}}},
		{Instances: []TrialInstanceChoice{{ID: 202, UserID: 20}}},
	} {
		if _, err := CaptureSelectedTrial(ctx, source, limits, sourceName, sourceUUID, invalid); !errors.Is(err, ErrTrialScope) {
			t.Fatalf("invalid selection returned %v", err)
		}
	}
	usersOnly, err := CaptureSelectedTrial(ctx, source, limits, sourceName, sourceUUID, TrialScope{UserIDs: []int64{1, 20}})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range usersOnly.Tables {
		if table.Name == "instances" && len(table.Rows) != 0 {
			t.Fatal("choosing accounts alone included their instances")
		}
	}
	target := DrillTarget{Database: targetName, ServerUUID: os.Getenv("TRIAL_SCOPE_MYSQL_TARGET_UUID"),
		CheckIsolation: func(context.Context) error { return nil }}
	if target.ServerUUID == "" || target.ServerUUID == sourceUUID {
		t.Fatal("scope fixture requires independent source and target servers")
	}
	writer := open("TRIAL_SCOPE_MYSQL_TARGET_DSN")
	plan, err := RestoreTrial(ctx, writer, target, snapshot, Placement{Namespace: "scope-target", StorageClass: "scope-local"})
	if err != nil {
		t.Fatal(err)
	}
	reader := open("TRIAL_SCOPE_MYSQL_TARGET_READ_DSN")
	if err := VerifyDrill(ctx, reader, target, plan); err != nil {
		t.Fatalf("independent target readback: %v", err)
	}
}
