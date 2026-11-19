package systembackup

import (
	"context"
	"errors"
	"strings"
)

// Only the migration-owned, single-statement audit receipt trigger is accepted
// on a capture source. No executable SQL is copied into a backup. RestoreMySQL
// still rejects every executable target object, including this trigger.
func validateCaptureExecutables(ctx context.Context, q SQLQueryer) error {
	var unexpected int
	if err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE())+(SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE())`).Scan(&unexpected); err != nil {
		return err
	}
	if unexpected != 0 {
		return errors.New("unsupported_schema")
	}
	rows, err := q.QueryContext(ctx, `SELECT TRIGGER_NAME,EVENT_MANIPULATION,EVENT_OBJECT_TABLE,ACTION_TIMING,ACTION_ORIENTATION,ACTION_STATEMENT,DEFINER=CURRENT_USER(),EVENT_OBJECT_SCHEMA=DATABASE() FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, event, table, timing, orientation, body string
		var ownDefiner, ownSchema bool
		if err = rows.Scan(&name, &event, &table, &timing, &orientation, &body, &ownDefiner, &ownSchema); err != nil {
			return err
		}
		// Normalize whitespace only. Quoting, comments, extra statements, alternate
		// expressions and definer accounts do not broaden this exact allowlist.
		body = strings.Join(strings.Fields(body), " ")
		expected := map[string][2]string{
			"observation_audit_enqueue":      {"audit_events", `INSERT IGNORE INTO observation_audit_pending(id) VALUES(NEW.id)`},
			"observation_invocation_enqueue": {"model_invocations", `INSERT IGNORE INTO observation_invocation_pending(id) VALUES(NEW.id)`},
			"observation_cost_enqueue":       {"cost_records", `INSERT IGNORE INTO observation_cost_pending(id) VALUES(NEW.id)`},
			"observation_chat_enqueue":       {"chat_messages", `INSERT IGNORE INTO observation_chat_pending(id) VALUES(NEW.id)`},
			"observation_risk_enqueue":       {"risk_hits", `INSERT IGNORE INTO observation_risk_pending(id) VALUES(NEW.id)`},
		}
		contract, ok := expected[name]
		if !ok || event != "INSERT" || table != contract[0] || timing != "AFTER" || orientation != "ROW" || !ownDefiner || !ownSchema || body != contract[1] {
			return errors.New("unsupported_schema")
		}
	}
	return rows.Err()
}
