-- Additive execution receipts. Existing rollout records remain readable.
ALTER TABLE runtime_upgrade_items
  ADD COLUMN operation_token VARCHAR(64) NULL,
  ADD COLUMN operation_phase VARCHAR(32) NULL,
  ADD COLUMN operation_started_at DATETIME(6) NULL,
  ADD COLUMN operation_deadline_at DATETIME(6) NULL,
  ADD COLUMN operation_attempts INT NOT NULL DEFAULT 0;
