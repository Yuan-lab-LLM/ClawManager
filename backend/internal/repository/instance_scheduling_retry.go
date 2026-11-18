package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"time"
)

// RetryRuntimeSchedulingError changes only the exact unbound error snapshot.
// It does not release a writer, remove an assignment, or advance a generation.
func (r *instanceRepository) RetryRuntimeSchedulingError(ctx context.Context, expected models.Instance) (bool, error) {
	if expected.ID < 1 || expected.RuntimeGeneration < 1 || expected.Status != "error" || expected.RuntimeErrorCode == nil || expected.RuntimeErrorMessage == nil || expected.UpdatedAt.IsZero() {
		return false, fmt.Errorf("invalid scheduling retry snapshot")
	}
	switch *expected.RuntimeErrorCode {
	case "RUNTIME_POOL_UNAVAILABLE", "RUNTIME_CAPACITY_EXHAUSTED", "CONTROL_PLANE_UNAVAILABLE", "GATEWAY_START_FAILED", "PROXY_POLICY_NOT_READY", "MODEL_CONFIGURATION_REQUIRED":
	default:
		return false, nil
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='creating',runtime_error_code=NULL,runtime_error_message=NULL,updated_at=? WHERE id=? AND user_id=? AND runtime_generation=? AND status='error' AND CAST(runtime_error_code AS BINARY)=CAST(? AS BINARY) AND CAST(runtime_error_message AS BINARY)=CAST(? AS BINARY) AND updated_at=? AND NOT EXISTS (SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id=instances.id)`, time.Now().UTC(), expected.ID, expected.UserID, expected.RuntimeGeneration, *expected.RuntimeErrorCode, *expected.RuntimeErrorMessage, expected.UpdatedAt)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
