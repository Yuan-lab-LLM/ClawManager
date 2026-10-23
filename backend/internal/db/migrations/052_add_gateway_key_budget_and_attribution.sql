SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'budget_limit') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN budget_limit DECIMAL(18,8) NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'budget_period') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN budget_period VARCHAR(16) NOT NULL DEFAULT ''lifetime''',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'budget_spent') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN budget_spent DECIMAL(18,8) NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'budget_reset_at') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN budget_reset_at TIMESTAMP NULL',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS gateway_key_budget_reservations (
  reservation_id VARCHAR(64) PRIMARY KEY,
  gateway_key_id BIGINT NOT NULL,
  reserved_cost DECIMAL(18,8) NOT NULL,
  actual_cost DECIMAL(18,8) NULL,
  settlement_status VARCHAR(16) NOT NULL DEFAULT 'reserved',
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_gateway_key_budget_reservations_key_expiry (gateway_key_id, expires_at),
  CONSTRAINT fk_gateway_key_budget_reservations_key FOREIGN KEY (gateway_key_id) REFERENCES gateway_keys(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_key_budget_reservations' AND COLUMN_NAME = 'actual_cost') = 0,
  'ALTER TABLE gateway_key_budget_reservations ADD COLUMN actual_cost DECIMAL(18,8) NULL AFTER reserved_cost',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_key_budget_reservations' AND COLUMN_NAME = 'settlement_status') = 0,
  'ALTER TABLE gateway_key_budget_reservations ADD COLUMN settlement_status VARCHAR(16) NOT NULL DEFAULT ''reserved'' AFTER actual_cost',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS gateway_key_budget_ledger (
  reservation_id VARCHAR(64) PRIMARY KEY,
  gateway_key_id BIGINT NOT NULL,
  actual_cost DECIMAL(18,8) NOT NULL,
  recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_gateway_key_budget_ledger_key_time (gateway_key_id, recorded_at),
  CONSTRAINT fk_gateway_key_budget_ledger_key FOREIGN KEY (gateway_key_id) REFERENCES gateway_keys(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'model_invocations' AND COLUMN_NAME = 'gateway_key_id') = 0,
  'ALTER TABLE model_invocations ADD COLUMN gateway_key_id BIGINT NULL',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'model_invocations' AND INDEX_NAME = 'idx_model_invocations_gateway_key_id') = 0,
  'ALTER TABLE model_invocations ADD INDEX idx_model_invocations_gateway_key_id (gateway_key_id)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'audit_events' AND COLUMN_NAME = 'gateway_key_id') = 0,
  'ALTER TABLE audit_events ADD COLUMN gateway_key_id BIGINT NULL',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'audit_events' AND INDEX_NAME = 'idx_audit_events_gateway_key_id') = 0,
  'ALTER TABLE audit_events ADD INDEX idx_audit_events_gateway_key_id (gateway_key_id)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'cost_records' AND COLUMN_NAME = 'gateway_key_id') = 0,
  'ALTER TABLE cost_records ADD COLUMN gateway_key_id BIGINT NULL',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'cost_records' AND INDEX_NAME = 'idx_cost_records_gateway_key_id') = 0,
  'ALTER TABLE cost_records ADD INDEX idx_cost_records_gateway_key_id (gateway_key_id)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
