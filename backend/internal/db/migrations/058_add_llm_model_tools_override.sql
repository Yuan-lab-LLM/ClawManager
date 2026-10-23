-- Preserve unknown discovery metadata; do not mark every old model incapable.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'tools_support_override') = 0,
  'ALTER TABLE llm_models ADD COLUMN tools_support_override BOOLEAN NULL DEFAULT NULL AFTER supports_tools',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
