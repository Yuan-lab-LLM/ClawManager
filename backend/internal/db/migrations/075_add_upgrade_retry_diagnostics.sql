ALTER TABLE runtime_upgrade_items
  ADD COLUMN operation_failures INT NOT NULL DEFAULT 0,
  ADD COLUMN operation_next_retry_at DATETIME(6) NULL,
  ADD COLUMN operation_last_error TEXT NULL;
