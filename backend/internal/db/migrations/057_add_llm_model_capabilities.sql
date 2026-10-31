SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'supports_tools') = 0,
  'ALTER TABLE llm_models ADD COLUMN supports_tools BOOLEAN NOT NULL DEFAULT FALSE AFTER reasoning_enabled',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'supports_vision') = 0,
  'ALTER TABLE llm_models ADD COLUMN supports_vision BOOLEAN NOT NULL DEFAULT FALSE AFTER supports_tools',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'capability_source') = 0,
  'ALTER TABLE llm_models ADD COLUMN capability_source VARCHAR(64) NOT NULL DEFAULT '''' AFTER supports_vision',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
