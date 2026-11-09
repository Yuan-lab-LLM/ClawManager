package repository

import (
	"context"
	"fmt"
	"github.com/upper/db/v4"
)

// The pending journal is the durable fence. It selects individual immutable
// source IDs, so retained siblings remain available as migration destinations.
// Caller holds LockRuntimeManagementAdmission for mutation/publication paths.
func CheckRuntimePodScaleInAdmission(ctx context.Context, tx db.Session, podID int64) error {
	var count int
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_management_operations WHERE kind='scale_in' AND status NOT IN ('completed','failed','cancelled') AND JSON_CONTAINS(JSON_EXTRACT(request_json,'$.plan.source_pods'),JSON_OBJECT('id',?))`, podID)
	if err == nil {
		err = row.Scan(&count)
	}
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("Runtime Pod is fenced as a scale-in source")
	}
	return nil
}
