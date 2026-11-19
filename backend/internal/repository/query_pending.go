package repository

import (
	"context"
	"fmt"
	"github.com/upper/db/v4"
	"strings"
)

// The trigger queue commits with the source, independently of allocated ID
// order. Each side of this merge and the selected work batch is bounded.
func observationCatchupIDs(ctx context.Context, tx db.Session, kind string, after, limit int) ([]interface{}, error) {
	queue, err := observationPendingTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := tx.SQL().QueryContext(ctx, "SELECT id FROM ((SELECT id FROM `"+kind+"` WHERE id>? ORDER BY id LIMIT ?) UNION (SELECT id FROM `"+queue+"` ORDER BY id LIMIT ?)) pending ORDER BY id LIMIT ?", after, limit, limit, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []interface{}
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func observationPendingTable(kind string) (string, error) {
	switch kind {
	case "model_invocations":
		return "observation_invocation_pending", nil
	case "cost_records":
		return "observation_cost_pending", nil
	case "audit_events":
		return "observation_audit_pending", nil
	}
	return "", fmt.Errorf("unsupported pending source %s", kind)
}
func clearObservationPending(ctx context.Context, tx db.Session, kind string, ids ...interface{}) error {
	if len(ids) == 0 {
		return nil
	}
	queue, err := observationPendingTable(kind)
	if err != nil {
		return err
	}
	_, err = tx.SQL().ExecContext(ctx, "DELETE FROM `"+queue+"` WHERE id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", ids...)
	return err
}
