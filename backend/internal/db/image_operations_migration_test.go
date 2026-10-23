package db

import (
	"strings"
	"testing"
)

func TestImageOperationsMigrationIsAdditiveAndRetrySafe(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/070_add_llm_model_image_operations.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, column := range []string{"model_kind", "image_generation_enabled", "image_edit_enabled"} {
		if !strings.Contains(sql, "COLUMN_NAME = '"+column+"'") || !strings.Contains(sql, "ADD COLUMN "+column) {
			t.Fatal("missing guarded column", column)
		}
	}
	if len(splitSQLStatements(sql)) != 12 || strings.Contains(strings.ToUpper(sql), "UPDATE LLM_MODELS") || !strings.Contains(sql, "DEFAULT ''chat''") {
		t.Fatal("migration must preserve legacy model behavior")
	}
}

func TestImageExecutionMigrations(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/071_add_image_budget_price.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	if !strings.Contains(sql, "COLUMN_NAME = 'image_price_per_image'") || !strings.Contains(sql, "DECIMAL(18,8) NULL") || strings.Contains(sql, "UPDATE llm_models") {
		t.Fatal("price must be additive and unset by default")
	}
	if len(splitSQLStatements(sql)) != 4 {
		t.Fatal("invalid price migration")
	}
	raw, err = embeddedMigrations.ReadFile("migrations/072_add_gateway_image_artifacts.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql = string(raw)
	for _, field := range []string{"CREATE TABLE IF NOT EXISTS", "user_id", "instance_id", "gateway_key_id", "model_id", "encryption_key", "expires_at", "ready"} {
		if !strings.Contains(sql, field) {
			t.Fatal("missing artifact security field", field)
		}
	}
	if len(splitSQLStatements(sql)) != 1 {
		t.Fatal("invalid artifact migration")
	}
}

func TestImageJobMigrationDurableDeduplication(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/073_add_gateway_image_jobs.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, expected := range []string{"CREATE TABLE IF NOT EXISTS", "UNIQUE KEY idx_image_job_idempotency (scope_hash,idempotency_hash)", "request_hash", "reservation_id", "deadline_at", "result_metadata", "idx_image_job_settlement"} {
		if !strings.Contains(sql, expected) {
			t.Fatal("missing durable job field", expected)
		}
	}
	if len(splitSQLStatements(sql)) != 1 {
		t.Fatal("invalid job migration")
	}
}

func TestImageAnalyticsMigrationPreservesLegacyRecords(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/074_add_image_analytics_outbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, column := range []string{"analytics_context", "analytics_outcome", "analytics_recorded"} {
		if !strings.Contains(sql, "COLUMN_NAME = '"+column+"'") || !strings.Contains(sql, "ADD COLUMN "+column) {
			t.Fatal("missing guarded column", column)
		}
	}
	if len(splitSQLStatements(sql)) != 16 || !strings.Contains(sql, "DEFAULT TRUE") || strings.Contains(strings.ToUpper(sql), "UPDATE GATEWAY_IMAGE_JOBS") || !strings.Contains(sql, "INDEX_NAME = 'idx_image_job_analytics'") {
		t.Fatal("migration must preserve old jobs without replaying legacy records")
	}
}
