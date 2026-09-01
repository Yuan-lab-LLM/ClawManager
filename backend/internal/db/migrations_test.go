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

func TestMigration036AddsReliableTeamEventOutbox(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/036_add_team_event_outbox.sql")
	if err != nil {
		t.Fatalf("read migration 036: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS team_event_outbox",
		"uk_team_event_outbox_message",
		"idx_team_event_outbox_pending",
		"source_event_id",
		"available_at",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 036 must contain %s", required)
		}
	}
}

func TestMigration037AddsTeamWorkflowLedger(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/037_add_team_workflow_ledger.sql")
	if err != nil {
		t.Fatalf("read migration 037: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"workflow_state",
		"plan_version",
		"ledger_version",
		"accepted_completion_id",
		"assignment_id",
		"canonical_work_id",
		"phase_id",
		"required_for_root",
		"CREATE TABLE IF NOT EXISTS team_workflow_phases",
		"uk_team_workflow_phase",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 037 must contain %s", required)
		}
	}
}

func TestMigration038AddsGatewayTokenAliases(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/038_add_instance_gateway_token_aliases.sql")
	if err != nil {
		t.Fatalf("read migration 038: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS instance_gateway_token_aliases",
		"token_hash CHAR(64)",
		"expires_at TIMESTAMP NOT NULL",
		"last_used_at TIMESTAMP NULL",
		"uk_instance_gateway_token_aliases_hash",
		"ON DELETE CASCADE",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 038 must contain %s", required)
		}
	}
	if strings.Contains(sql, "access_token") {
		t.Fatalf("migration 038 must not store raw access tokens")
	}
}

func TestMigration041AddsSessionUsageIndexes(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/041_add_session_usage_indexes.sql")
	if err != nil {
		t.Fatalf("read migration 041: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"idx_cost_records_instance_id",
		"idx_cost_records_session_id",
		"idx_model_invocations_instance_session",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 041 must contain %s", required)
		}
	}
}

func TestMigration042AddsImmutableReviewContractTarget(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/042_add_team_review_contract.sql")
	if err != nil {
		t.Fatalf("read migration 042: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"review_target_assignment_id",
		"review_target_revision",
		"idx_team_work_items_review_target",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 042 must contain %s", required)
		}
	}
}

func TestMigration044AddsWorkbuddyRuntime(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/044_add_workbuddy_instance_type.sql")
	if err != nil {
		t.Fatalf("read migration 044: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"'workbuddy'",
		"instance_type = 'workbuddy'",
		"LOWER(TRIM(display_name)) = 'workbuddy'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 044 must contain %s", required)
		}
	}
}

func TestMigration045BootstrapsAndUpgradesLLMModels(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/045_add_llm_model_reasoning_control.sql")
	if err != nil {
		t.Fatalf("read migration 045: %v", err)
	}

	sql := string(raw)
	createTable := "CREATE TABLE IF NOT EXISTS llm_models"
	guardColumn := "information_schema.COLUMNS"
	addColumn := "ALTER TABLE llm_models ADD COLUMN reasoning_enabled"
	for _, required := range []string{
		createTable,
		guardColumn,
		"TABLE_NAME = 'llm_models'",
		"COLUMN_NAME = 'reasoning_enabled'",
		addColumn,
		"PREPARE stmt FROM @stmt",
		"EXECUTE stmt",
		"DEALLOCATE PREPARE stmt",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 045 must contain %s", required)
		}
	}

	if strings.Index(sql, createTable) > strings.Index(sql, addColumn) {
		t.Fatalf("migration 045 must create the legacy llm_models table before adding reasoning_enabled")
	}
}

func TestMigration047AddsDeepSeekHarnessRuntimes(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/047_add_deepseek_harness_runtime.sql")
	if err != nil {
		t.Fatalf("read migration 047: %v", err)
	}

	sql := string(raw)
	for _, required := range []string{
		"'deepseek-harness'",
		"'desktop'",
		"'gateway'",
		"'DeepSeek Harness Pro'",
		"'DeepSeek Harness Lite'",
		"ghcr.io/yuan-lab-llm/agentsruntime/deepseek-harness:latest",
		"ghcr.io/yuan-lab-llm/agentsruntime/deepseek-harness-lite:latest",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 047 must contain %s", required)
		}
	}
}

func TestMigration048PreservesAllInstanceTypes(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/048_fix_instance_type_enum.sql")
	if err != nil {
		t.Fatalf("read migration 048: %v", err)
	}

	sql := string(raw)
	for _, instanceType := range []string{
		"'openclaw'",
		"'ubuntu'",
		"'debian'",
		"'centos'",
		"'custom'",
		"'webtop'",
		"'hermes'",
		"'workbuddy'",
		"'opencode'",
		"'deepseek-harness'",
	} {
		if !strings.Contains(sql, instanceType) {
			t.Fatalf("migration 048 must preserve instance type %s", instanceType)
		}
	}
}

func TestMigration049BackfillsLDAPLoginAliasesWithJoin(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/049_add_ldap_login_alias.sql")
	if err != nil {
		t.Fatalf("read migration 049: %v", err)
	}

	sql := string(raw)
	for _, required := range []string{
		"ALTER TABLE users ADD COLUMN login_alias",
		"UPDATE users AS u\nJOIN",
		"AS unique_ldap_usernames ON unique_ldap_usernames.username = u.username",
		"HAVING COUNT(*) = 1",
		"local_username_key",
		"uk_users_local_username",
		"uk_users_provider_login_alias",
		"uk_users_provider_external_id",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 049 must contain %s", required)
		}
	}
	if strings.Contains(sql, "SELECT COUNT(*) FROM users AS same_uid") {
		t.Fatalf("migration 049 must not use a same-table correlated subquery in the update")
	}
}

func TestMigration050IncludesLDAPTLSCertificateSettings(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/050_add_enterprise_auth_settings.sql")
	if err != nil {
		t.Fatalf("read migration 050: %v", err)
	}

	sql := string(raw)
	for _, required := range []string{
		"ldap_tls_ca_file VARCHAR(1000) NOT NULL DEFAULT ''",
		"ldap_tls_server_name VARCHAR(255) NOT NULL DEFAULT ''",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 050 must contain %s", required)
		}
	}
}
