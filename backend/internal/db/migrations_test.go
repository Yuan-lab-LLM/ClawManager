package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitSQLStatements(t *testing.T) {
	input := `
-- create table comment
CREATE TABLE demo (
  id INT PRIMARY KEY,
  note VARCHAR(255) DEFAULT 'hello;world'
);

/* block comment */
INSERT INTO demo (id, note) VALUES (1, 'value');
UPDATE demo SET note = "a;quoted" WHERE id = 1;
`

	got := splitSQLStatements(input)
	want := []string{
		"CREATE TABLE demo (\n  id INT PRIMARY KEY,\n  note VARCHAR(255) DEFAULT 'hello;world'\n)",
		"INSERT INTO demo (id, note) VALUES (1, 'value')",
		`UPDATE demo SET note = "a;quoted" WHERE id = 1`,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected statements:\nwant: %#v\ngot: %#v", want, got)
	}
}

func TestMigration023IsEmbedded(t *testing.T) {
	files, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	found := false
	for _, file := range files {
		if file.Name() == "023_add_runtime_pool_v2.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration 023_add_runtime_pool_v2.sql is not embedded")
	}
}

func TestMigration034UpdatesLiteDefaultImages(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/034_update_lite_default_images.sql")
	if err != nil {
		t.Fatalf("read migration 034: %v", err)
	}
	sql := string(raw)
	for _, image := range []string{
		"ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest",
		"ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest",
	} {
		if !strings.Contains(sql, image) {
			t.Fatalf("migration 034 must update lite image %s", image)
		}
	}
}

func TestMigration023IsRetrySafe(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/023_add_runtime_pool_v2.sql")
	if err != nil {
		t.Fatalf("read migration 023: %v", err)
	}

	sql := string(raw)
	if !strings.Contains(sql, "information_schema.COLUMNS") {
		t.Fatalf("migration 023 must guard instance column additions with information_schema.COLUMNS")
	}
	for _, column := range []string{
		"workspace_path",
		"workspace_usage_bytes",
		"runtime_generation",
		"runtime_error_message",
	} {
		if !strings.Contains(sql, "COLUMN_NAME = '"+column+"'") {
			t.Fatalf("migration 023 must guard %s column addition", column)
		}
	}
	for _, table := range []string{
		"runtime_pods",
		"instance_runtime_bindings",
		"runtime_rollouts",
		"workspace_file_audits",
	} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration 023 must create %s idempotently", table)
		}
	}
}

func TestMigration035HardensTeamEventProtocol(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/035_harden_team_event_protocol.sql")
	if err != nil {
		t.Fatalf("read migration 035: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"event_id",
		"completion_id",
		"sequence_no",
		"uk_team_events_event_id",
		"uk_team_events_completion_id",
		"CREATE TABLE IF NOT EXISTS team_work_items",
		"uk_team_work_items_work",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 035 must contain %s", required)
		}
	}
}
