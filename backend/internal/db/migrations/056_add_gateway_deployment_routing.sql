SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'max_concurrent_requests') = 0,
  'ALTER TABLE llm_models ADD COLUMN max_concurrent_requests INT NOT NULL DEFAULT 0 AFTER deployment_priority',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'model_invocations' AND COLUMN_NAME = 'ttft_ms') = 0,
  'ALTER TABLE model_invocations ADD COLUMN ttft_ms INT NULL AFTER latency_ms',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'model_invocations' AND COLUMN_NAME = 'generation_ms') = 0,
  'ALTER TABLE model_invocations ADD COLUMN generation_ms INT NULL AFTER ttft_ms',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'model_invocations' AND COLUMN_NAME = 'tokens_per_second') = 0,
  'ALTER TABLE model_invocations ADD COLUMN tokens_per_second DECIMAL(12,4) NULL AFTER generation_ms',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'model_invocations' AND COLUMN_NAME = 'attempt_no') = 0,
  'ALTER TABLE model_invocations ADD COLUMN attempt_no INT NOT NULL DEFAULT 1 AFTER tokens_per_second',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
