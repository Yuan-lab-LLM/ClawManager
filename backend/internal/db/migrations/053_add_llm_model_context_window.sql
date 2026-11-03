SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'llm_models'
      AND COLUMN_NAME = 'context_window_tokens') = 0,
  'ALTER TABLE llm_models ADD COLUMN context_window_tokens INT NOT NULL DEFAULT 0 AFTER deployment_priority',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
