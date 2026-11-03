package db

import (
	"strings"
	"testing"
)

func TestMigration055QuarantinesLegacyCurrencyWithoutOverwritingReview(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/055_gateway_budget_currency_review.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	if len(splitSQLStatements(sql)) != 9 {
		t.Fatal("currency migration parsing failed")
	}
	for _, fragment := range []string{"information_schema.COLUMNS", "budget_currency_review_required", "FROM cost_records", "c.gateway_key_id = k.id", "<> 'USD'", "WHERE k.budget_currency_reviewed_at IS NULL"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("missing %s", fragment)
		}
	}
	statements := splitSQLStatements(sql)
	if !strings.HasPrefix(statements[len(statements)-1], "UPDATE gateway_keys") {
		t.Fatal("backfill must run on retry after DDL autocommit")
	}
}
