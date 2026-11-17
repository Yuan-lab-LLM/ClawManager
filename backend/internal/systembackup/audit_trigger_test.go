package systembackup

import (
	"context"
	"testing"
)

func TestAuditTriggerCaptureAllowlistAndInertRestore(t *testing.T) {
	source := fullSchemaDB(t)
	ctx := context.Background()
	if err := ValidateSchema(ctx, source); err != nil {
		t.Fatalf("trusted migrated audit enqueue rejected: %v", err)
	}
	if err := ValidateTeamDataSchema(ctx, source); err != nil {
		t.Fatal(err)
	}
	target, _ := isolatedSchemaDB(t, source)
	var n int
	if err := target.QueryRow(`SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("data-only restore has executable trigger: %d %v", n, err)
	}
	for _, q := range []string{`DROP TRIGGER observation_audit_enqueue`, `CREATE TRIGGER observation_audit_enqueue AFTER INSERT ON audit_events FOR EACH ROW INSERT IGNORE INTO observation_audit_pending(id) VALUES(NEW.id+1)`} {
		if _, err := source.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateSchema(ctx, source); err == nil {
		t.Fatal("changed trusted trigger body accepted")
	}
	if err := ValidateTeamDataSchema(ctx, source); err == nil {
		t.Fatal("Team accepted changed trusted trigger body")
	}
	for _, q := range []string{`DROP TRIGGER observation_audit_enqueue`, `CREATE TRIGGER unknown_enqueue AFTER INSERT ON audit_events FOR EACH ROW INSERT IGNORE INTO observation_audit_pending(id) VALUES(NEW.id)`} {
		if _, err := source.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateSchema(ctx, source); err == nil {
		t.Fatal("foreign trigger accepted")
	}
}
