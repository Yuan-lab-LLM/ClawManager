-- Core-only additive state. No new northbound Gateway grants.
SET @batch_policy_sql = IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='instance_lifecycle_batches' AND COLUMN_NAME='policy_version')=0,
 'ALTER TABLE instance_lifecycle_batches ADD COLUMN policy_version INT NOT NULL DEFAULT 1, ADD COLUMN retry_round INT NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE batch_policy_stmt FROM @batch_policy_sql;
EXECUTE batch_policy_stmt;
DEALLOCATE PREPARE batch_policy_stmt;

-- Original operations remain immutable evidence. A second attempt gets its own ID.
CREATE TABLE IF NOT EXISTS instance_lifecycle_batch_attempts (
 item_id BIGINT NOT NULL,
 attempt INT NOT NULL,
 operation_id VARCHAR(64) NOT NULL,
 PRIMARY KEY (item_id, attempt),
 UNIQUE KEY uk_lifecycle_attempt_operation (operation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
