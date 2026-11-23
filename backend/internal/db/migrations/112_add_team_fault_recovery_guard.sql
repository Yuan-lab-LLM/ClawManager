ALTER TABLE team_runtime_lifecycle
    ADD COLUMN fault_recovery_operation_id VARCHAR(64) NOT NULL DEFAULT '';
