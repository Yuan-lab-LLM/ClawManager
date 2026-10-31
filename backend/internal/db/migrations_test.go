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

func TestOpenClaw81UpgradeMigrationsAreEmbeddedAndDataSafe(t *testing.T) {
	safety, err := embeddedMigrations.ReadFile("migrations/058_add_openclaw_runtime_upgrade_safety.sql")
	if err != nil {
		t.Fatal(err)
	}
	safetySQL := string(safety)
	for _, contract := range []string{"runtime_upgrade_items", "runtime_upgrade_audits", "preflight_id", "capabilities_json", "rollback_status"} {
		if !strings.Contains(safetySQL, contract) {
			t.Fatalf("migration 058 missing %q", contract)
		}
	}
	pin, err := embeddedMigrations.ReadFile("migrations/059_pin_openclaw_2026_8_1_images.sql")
	if err != nil {
		t.Fatal(err)
	}
	pinSQL := string(pin)
	if !strings.Contains(pinSQL, "2026.8.1") || !strings.Contains(pinSQL, "image IN") {
		t.Fatal("migration 059 must update only known previous defaults to 2026.8.1")
	}
	for _, destructive := range []string{"DROP TABLE", "TRUNCATE", "DELETE FROM instances", "DELETE FROM runtime_pods"} {
		if strings.Contains(strings.ToUpper(safetySQL+pinSQL), destructive) {
			t.Fatalf("OpenClaw upgrade migration contains destructive statement %q", destructive)
		}
	}
	release, err := embeddedMigrations.ReadFile("migrations/060_allow_terminal_runtime_upgrade_instance_deletion.sql")
	if err != nil {
		t.Fatal(err)
	}
	releaseSQL := string(release)
	if !strings.Contains(releaseSQL, "DROP FOREIGN KEY fk_runtime_upgrade_item_instance") {
		t.Fatal("migration 060 must release only the historical instance deletion blocker")
	}
	for _, destructive := range []string{"DELETE FROM", "DROP TABLE", "TRUNCATE"} {
		if strings.Contains(strings.ToUpper(releaseSQL), destructive) {
			t.Fatalf("migration 060 must preserve audit and user rows; found %q", destructive)
		}
	}
	sourceIdentity, err := embeddedMigrations.ReadFile("migrations/061_persist_runtime_upgrade_source_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	sourceIdentitySQL := string(sourceIdentity)
	for _, required := range []string{"source_gateway_id", "source_generation", "source_pod_uid", "source_deployment_name", "source_image_digest"} {
		if !strings.Contains(sourceIdentitySQL, required) {
			t.Fatalf("migration 061 missing %q", required)
		}
	}
	for _, destructive := range []string{"DELETE FROM", "DROP TABLE", "TRUNCATE"} {
		if strings.Contains(strings.ToUpper(sourceIdentitySQL), destructive) {
			t.Fatalf("migration 061 must preserve existing rollout and user rows; found %q", destructive)
		}
	}
	followLatest, err := embeddedMigrations.ReadFile("migrations/062_follow_openclaw_latest_images.sql")
	if err != nil {
		t.Fatal(err)
	}
	followLatestSQL := string(followLatest)
	for _, required := range []string{
		"ghcr.io/yuan-lab-llm/agentsruntime/openclaw:2026.8.1",
		"ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest",
		"ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:2026.8.1",
		"ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest",
		"image IN",
	} {
		if !strings.Contains(followLatestSQL, required) {
			t.Fatalf("migration 062 missing %q", required)
		}
	}
	for _, destructive := range []string{"DELETE FROM", "DROP TABLE", "TRUNCATE"} {
		if strings.Contains(strings.ToUpper(followLatestSQL), destructive) {
			t.Fatalf("migration 062 must preserve custom image settings and user rows; found %q", destructive)
		}
	}
}

func TestMigration034UpdatesLiteDefaultImages(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/034_update_lite_default_images.sql")
	if err != nil {
		t.Fatalf("read migration 034: %v", err)
	}
	sql := strings.ReplaceAll(string(raw), "\r\n", "\n")
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

func TestMigration048AddsWorkbuddyRuntime(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/048_add_workbuddy_instance_type.sql")
	if err != nil {
		t.Fatalf("read migration 048: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"'workbuddy'",
		"instance_type = 'workbuddy'",
		"LOWER(TRIM(display_name)) = 'workbuddy'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 048 must contain %s", required)
		}
	}
}

func TestManagedRuntimeEnumMigrationsPreserveAllRuntimeTypes(t *testing.T) {
	files := []string{
		"044_add_workbuddy_instance_type.sql",
		"045_add_opencode_instance_type.sql",
		"046_add_opencode_lite_runtime.sql",
		"047_add_deepseek_harness_runtime.sql",
		"048_add_workbuddy_instance_type.sql",
		"049_add_opencode_instance_type.sql",
		"051_add_codex_and_claude_code_instance_types.sql",
		"055_reconcile_instance_type_enum.sql",
	}
	requiredTypes := []string{"'workbuddy'", "'opencode'", "'deepseek-harness'", "'codex'", "'claude-code'"}
	for _, name := range files {
		raw, err := embeddedMigrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		sql := string(raw)
		for _, instanceType := range requiredTypes {
			if !strings.Contains(sql, instanceType) {
				t.Fatalf("migration %s must preserve instance type %s", name, instanceType)
			}
		}
	}
}

func TestMigration050UpdatesWorkbuddyWindowsRuntime(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/050_update_workbuddy_windows_runtime.sql")
	if err != nil {
		t.Fatalf("read migration 050: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"windows-vm-workbuddy:latest",
		"runtime_type = 'desktop'",
		"instance_type = 'workbuddy'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 050 must contain %s", required)
		}
	}
}

func TestMigration045AddsNorthboundSecurityState(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/045_add_northbound_api.sql")
	if err != nil {
		t.Fatalf("read migration 045: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS northbound_auth_challenges",
		"CREATE TABLE IF NOT EXISTS northbound_sessions",
		"previous_refresh_token_hash",
		"refresh_token_history",
		"CREATE TABLE IF NOT EXISTS northbound_operations",
		"uk_northbound_operation_idempotency",
		"provisioning_operation_id",
		"uk_instances_provisioning_operation",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 045 must contain %s", required)
		}
	}
}

func TestMigration047AddsInstanceOwner(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/047_add_instance_owner.sql")
	if err != nil {
		t.Fatalf("read migration 047: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"ADD COLUMN owner VARCHAR(128)",
		"COLLATE utf8mb4_bin",
		"owner_normalized",
		"GENERATED ALWAYS AS (LOWER(TRIM(owner))) STORED",
		"idx_instances_user_owner_mode_created",
		"user_id, owner, instance_mode, created_at, id",
		"idx_instances_owner_normalized_mode_created",
		"owner_normalized, instance_mode, created_at, id",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 047 must contain %s", required)
		}
	}
}

func TestMigration049AddsOpenCodeInstanceType(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/049_add_opencode_instance_type.sql")
	if err != nil {
		t.Fatalf("read migration 049: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"MODIFY COLUMN type ENUM",
		"'workbuddy'",
		"'opencode'",
		"'gateway'",
		"'desktop'",
		"OpenCode Lite",
		"OpenCode Pro",
		"agentsruntime/opencode-lite:latest",
		"agentsruntime/opencode:latest",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 049 must contain %s", required)
		}
	}
}

func TestMigration052AddsInstancePVCName(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/052_add_instance_pvc_name.sql")
	if err != nil {
		t.Fatalf("read migration 052: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{"ALTER TABLE instances", "pvc_name", "VARCHAR(253)", "information_schema.COLUMNS", "PREPARE instance_pvc_name_column_stmt"} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 052 must contain %s", required)
		}
	}
}

func TestMigration051AddsCodexAndClaudeCodeInstanceTypes(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/051_add_codex_and_claude_code_instance_types.sql")
	if err != nil {
		t.Fatalf("read migration 051: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"MODIFY COLUMN type ENUM",
		"'workbuddy'",
		"'codex'",
		"'claude-code'",
		"Codex Pro",
		"Claude Code Pro",
		"agentsruntime/codex:latest",
		"agentsruntime/claude-code:latest",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 051 must contain %s", required)
		}
	}
}

func TestMigration053AddsAndBackfillsInstanceRuntimeVariant(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/053_add_instance_runtime_variant.sql")
	if err != nil {
		t.Fatalf("read migration 053: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{"runtime_variant", "'linux'", "'windows'", "workbuddy-linux", "windows-vm-workbuddy", "information_schema.COLUMNS", "PREPARE instance_runtime_variant_column_stmt"} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 053 must contain %s", required)
		}
	}
}

func TestMigration054AddsAndBackfillsSystemImageRuntimeVariant(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/054_add_system_image_runtime_variant.sql")
	if err != nil {
		t.Fatalf("read migration 054: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"system_image_settings",
		"runtime_variant",
		"workbuddy-linux",
		"windows-vm-workbuddy",
		"windows-vm-codex",
		"agentsruntime/codex",
		"WHERE type = 'codex'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 054 must contain %s", required)
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

func TestMigration056AddsLLMProviderModelCatalog(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/056_add_llm_provider_models.sql")
	if err != nil {
		t.Fatalf("read migration 056: %v", err)
	}

	sql := string(raw)
	for _, required := range []string{
		"information_schema.COLUMNS",
		"TABLE_NAME = 'llm_models'",
		"COLUMN_NAME = 'provider_models_json'",
		"ALTER TABLE llm_models ADD COLUMN provider_models_json TEXT",
		"PREPARE stmt FROM @stmt",
		"EXECUTE stmt",
		"DEALLOCATE PREPARE stmt",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 056 must contain %s", required)
		}
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

func TestMigration058EnablesDeepSeekHarnessProWithoutOverwritingCustomPolicy(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/058_enable_deepseek_harness_pro.sql")
	if err != nil {
		t.Fatalf("read migration 058: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"JSON_LENGTH(allowed_pro_types) = 4",
		"JSON_CONTAINS(allowed_pro_types, JSON_QUOTE('openclaw'))",
		"JSON_ARRAY_APPEND(allowed_pro_types, '$', 'deepseek-harness')",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 058 must contain %s", required)
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

func TestMigration052AddsGatewayKeyBudgetAndAttribution(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/052_add_gateway_key_budget_and_attribution.sql")
	if err != nil {
		t.Fatalf("read migration 052: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"information_schema.COLUMNS",
		"information_schema.STATISTICS",
		"budget_limit",
		"budget_period",
		"budget_spent",
		"actual_cost",
		"settlement_status",
		"gateway_key_budget_reservations",
		"gateway_key_budget_ledger",
		"idx_model_invocations_gateway_key_id",
		"idx_audit_events_gateway_key_id",
		"idx_cost_records_gateway_key_id",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 052 must contain %s", required)
		}
	}
}

func TestMigration051IsRetrySafe(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/051_add_gateway_key_rate_limits.sql")
	if err != nil {
		t.Fatalf("read migration 051: %v", err)
	}
	sql := string(raw)
	if !strings.Contains(sql, "information_schema.COLUMNS") {
		t.Fatal("migration 051 must guard gateway key rate-limit columns")
	}
	for _, column := range []string{"rpm_limit", "tpm_limit", "concurrent_requests_limit"} {
		if !strings.Contains(sql, "COLUMN_NAME = '"+column+"'") {
			t.Fatalf("migration 051 must guard %s column addition", column)
		}
	}
}

func TestMigration052GuardsEveryAlteredColumnAndIndex(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/052_add_gateway_key_budget_and_attribution.sql")
	if err != nil {
		t.Fatalf("read migration 052: %v", err)
	}
	sql := string(raw)
	for _, column := range []string{
		"budget_limit", "budget_period", "budget_spent", "budget_reset_at",
		"actual_cost", "settlement_status", "gateway_key_id",
	} {
		if !strings.Contains(sql, "COLUMN_NAME = '"+column+"'") {
			t.Fatalf("migration 052 must guard %s column addition", column)
		}
	}
	for _, index := range []string{
		"idx_model_invocations_gateway_key_id",
		"idx_audit_events_gateway_key_id",
		"idx_cost_records_gateway_key_id",
	} {
		if !strings.Contains(sql, "INDEX_NAME = '"+index+"'") {
			t.Fatalf("migration 052 must guard %s index addition", index)
		}
	}
}

func TestMigration053AddsLLMModelContextWindow(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/053_add_llm_model_context_window.sql")
	if err != nil {
		t.Fatalf("read migration 053: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"information_schema.COLUMNS",
		"TABLE_NAME = 'llm_models'",
		"COLUMN_NAME = 'context_window_tokens'",
		"ALTER TABLE llm_models ADD COLUMN context_window_tokens",
		"PREPARE stmt FROM @stmt",
		"EXECUTE stmt",
		"DEALLOCATE PREPARE stmt",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 053 must contain %s", required)
		}
	}
}

func TestMigration056AddsDeploymentRoutingMetrics(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/056_add_gateway_deployment_routing.sql")
	if err != nil {
		t.Fatalf("read migration 056: %v", err)
	}
	sql := string(raw)
	for _, column := range []string{
		"max_concurrent_requests", "ttft_ms", "generation_ms", "tokens_per_second", "attempt_no",
	} {
		if !strings.Contains(sql, "COLUMN_NAME = '"+column+"'") {
			t.Fatalf("migration 056 must guard %s column addition", column)
		}
	}
	for _, required := range []string{
		"information_schema.COLUMNS",
		"ALTER TABLE llm_models ADD COLUMN max_concurrent_requests",
		"ALTER TABLE model_invocations ADD COLUMN ttft_ms",
		"ALTER TABLE model_invocations ADD COLUMN tokens_per_second",
		"PREPARE stmt FROM @stmt",
		"EXECUTE stmt",
		"DEALLOCATE PREPARE stmt",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 056 must contain %s", required)
		}
	}
}
