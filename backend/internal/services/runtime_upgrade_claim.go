package services

import (
	"clawreef/internal/models"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type upgradeClaimKey struct{}

func (s *RuntimeUpgradeService) runUpgradeItems(ctx context.Context, rolloutID int64, phase string, items []models.RuntimeUpgradeItem, concurrency int, run func(context.Context, int) error) error {
	if s.rollouts != nil && (phase == "compatibility_check" || phase == "session_migration" || phase == "gateway_restart") {
		r, err := s.rollouts.GetByID(ctx, rolloutID)
		if err != nil {
			return err
		}
		if r != nil && runtimeUpgradePolicy(r).Version >= 2 {
			return s.runConvergentUpgradeItems(ctx, r, phase, items, concurrency, run)
		}
	}
	return runUpgradeBounded(ctx, len(items), concurrency, func(ctx context.Context, index int) error {
		return s.withUpgradeClaim(ctx, rolloutID, items[index].InstanceID, phase, func(ctx context.Context) error { return run(ctx, index) })
	})
}

// A short, renewable controller claim prevents duplicate dispatch. It is not
// authority to overwrite a live Runtime writer: the cross-Pod operation lock
// and identity-bound lease are still required after controller takeover.
func (s *RuntimeUpgradeService) withUpgradeClaim(ctx context.Context, rolloutID int64, instanceID int, phase string, run func(context.Context) error) error {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(nonce[:])
	result, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET phase_entered_at=IF(COALESCE(operation_phase,'')=?,COALESCE(phase_entered_at,UTC_TIMESTAMP(6)),UTC_TIMESTAMP(6)),operation_failures=IF(COALESCE(operation_phase,'')=?,operation_failures,0),operation_next_retry_at=IF(COALESCE(operation_phase,'')=?,operation_next_retry_at,NULL),operation_token=?, operation_phase=?, operation_started_at=UTC_TIMESTAMP(6), operation_deadline_at=DATE_ADD(UTC_TIMESTAMP(6), INTERVAL 90 SECOND), operation_attempts=operation_attempts+1 WHERE rollout_id=? AND instance_id=? AND (operation_token IS NULL OR operation_deadline_at < UTC_TIMESTAMP(6)) AND (COALESCE(operation_phase,'')<>? OR operation_next_retry_at IS NULL OR operation_next_retry_at<=UTC_TIMESTAMP(6))`, phase, phase, phase, token, phase, rolloutID, instanceID, phase)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrRuntimeUpgradeBusy
	}
	operationCtx, cancel := context.WithCancel(context.WithValue(ctx, upgradeClaimKey{}, token))
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-operationCtx.Done():
				done <- nil
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(operationCtx, 5*time.Second)
				r, err := s.sess.SQL().ExecContext(renewCtx, `UPDATE runtime_upgrade_items SET operation_deadline_at=DATE_ADD(UTC_TIMESTAMP(6), INTERVAL 90 SECOND) WHERE rollout_id=? AND instance_id=? AND operation_token=?`, rolloutID, instanceID, token)
				renewCancel()
				if err == nil {
					var n int64
					n, err = r.RowsAffected()
					if err == nil && n != 1 {
						err = ErrRuntimeUpgradeBusy
					}
				}
				if err != nil {
					cancel()
					done <- err
					return
				}
			}
		}
	}()
	runErr := run(operationCtx)
	cancel()
	renewErr := <-done
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	// Claim renewals are not progress. Persist the real error before a generic
	// outer watchdog can hide it; transient failures back off and have a budget.
	if runErr != nil && !runtimeUpgradeReconcileInterrupted(ctx, runErr) && !errors.Is(runErr, ErrRuntimeUpgradeBusy) {
		_, diagnosticErr := s.sess.SQL().ExecContext(cleanupCtx, `UPDATE runtime_upgrade_items SET operation_failures=operation_failures+1,error_message=COALESCE(error_message,?),operation_last_error=?,operation_next_retry_at=CASE WHEN ? THEN DATE_ADD(UTC_TIMESTAMP(6),INTERVAL LEAST(30,5*POW(2,LEAST(operation_failures,3))) SECOND) ELSE NULL END WHERE rollout_id=? AND instance_id=? AND operation_token=?`, runErr.Error(), runErr.Error(), runtimeUpgradeInfrastructureRetryable(runErr), rolloutID, instanceID, token)
		var failures int
		row, readErr := s.sess.SQL().QueryRowContext(cleanupCtx, `SELECT operation_failures FROM runtime_upgrade_items WHERE rollout_id=? AND instance_id=?`, rolloutID, instanceID)
		if readErr == nil {
			readErr = row.Scan(&failures)
		}
		if failures >= 5 && runtimeUpgradeInfrastructureRetryable(runErr) {
			runErr = fmt.Errorf("%w: retry budget exhausted after %d failures: %v", ErrUpgradeSourceState, failures, runErr)
		}
		runErr = errors.Join(runErr, diagnosticErr, readErr)
	} else if runErr == nil {
		_, resetErr := s.sess.SQL().ExecContext(cleanupCtx, `UPDATE runtime_upgrade_items SET operation_failures=0,operation_next_retry_at=NULL WHERE rollout_id=? AND instance_id=? AND operation_token=?`, rolloutID, instanceID, token)
		runErr = errors.Join(runErr, resetErr)
	}
	_, releaseErr := s.sess.SQL().ExecContext(cleanupCtx, `UPDATE runtime_upgrade_items SET operation_token=NULL, operation_deadline_at=NULL WHERE rollout_id=? AND instance_id=? AND operation_token=?`, rolloutID, instanceID, token)
	return errors.Join(runErr, renewErr, releaseErr)
}

func (s *RuntimeUpgradeService) execClaimedUpgradeState(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if token, ok := ctx.Value(upgradeClaimKey{}).(string); ok {
		query += " AND operation_token = ?"
		args = append(args, token)
	}
	return s.sess.SQL().ExecContext(ctx, query, args...)
}
