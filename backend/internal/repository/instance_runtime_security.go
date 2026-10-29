package repository

import (
	"context"
	"fmt"
	"time"
)

// ProtectRuntimeGeneration is monotonic: assignment loss or rollout rollback
// must not reactivate an old long-lived bearer credential.
func (r *instanceRepository) ProtectRuntimeGeneration(ctx context.Context, id, generation int, profile string) error {
	if id < 1 || generation < 1 || profile != "isolated-network-v1" {
		return fmt.Errorf("invalid runtime security transition")
	}
	// Compare stored profiles byte-for-byte; MySQL text collations can equate
	// unknown whitespace/case variants with the allowed markers.
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE instances SET runtime_security_profile = ?, updated_at = ? WHERE id = ? AND runtime_generation = ? AND status = 'creating' AND CAST(runtime_security_profile AS BINARY) IN ('', ?)`, profile, time.Now().UTC(), id, generation, profile)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		current, readErr := r.GetByIDContext(ctx, id)
		if readErr != nil || current == nil || current.RuntimeGeneration != generation || current.Status != "creating" || current.RuntimeSecurityProfile != profile {
			return fmt.Errorf("runtime security transition lost assignment")
		}
	}
	return nil
}
