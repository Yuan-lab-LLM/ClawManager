-- Opt-in endpoint adapter. Existing records keep their legacy behavior.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'reasoning_control_mode') = 0,
  'ALTER TABLE llm_models ADD COLUMN reasoning_control_mode VARCHAR(40) NOT NULL DEFAULT '''' AFTER reasoning_enabled',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
