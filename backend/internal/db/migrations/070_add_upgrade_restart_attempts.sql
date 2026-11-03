-- Nullable/additive: legacy rolling upgrades do not use these fields.
ALTER TABLE runtime_upgrade_items
  ADD COLUMN recovery_started_at DATETIME(6) NULL,
  ADD COLUMN restart_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN restart_target_pod_uid VARCHAR(128) NULL,
  ADD COLUMN restart_started_at DATETIME(6) NULL,
  ADD COLUMN restart_deadline_at DATETIME(6) NULL,
  ADD COLUMN restart_next_retry_at DATETIME(6) NULL,
  ADD COLUMN recovery_status VARCHAR(32) NULL,
  ADD COLUMN recovery_error_code VARCHAR(64) NULL;
