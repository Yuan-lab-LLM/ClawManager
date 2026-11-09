ALTER TABLE runtime_upgrade_items
  ADD COLUMN isolated_at DATETIME(6) NULL,
  ADD COLUMN isolation_phase VARCHAR(40) NULL,
  ADD COLUMN phase_entered_at DATETIME(6) NULL;
