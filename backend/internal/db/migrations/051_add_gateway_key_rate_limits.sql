SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'rpm_limit') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN rpm_limit INT NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'tpm_limit') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN tpm_limit INT NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gateway_keys' AND COLUMN_NAME = 'concurrent_requests_limit') = 0,
  'ALTER TABLE gateway_keys ADD COLUMN concurrent_requests_limit INT NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
