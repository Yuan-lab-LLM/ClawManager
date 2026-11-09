-- Additive idempotency receipt for user-requested instance recovery.
ALTER TABLE runtime_rollouts ADD COLUMN recovery_key VARCHAR(96) NULL;
CREATE UNIQUE INDEX uk_runtime_rollouts_recovery_key ON runtime_rollouts(recovery_key);
