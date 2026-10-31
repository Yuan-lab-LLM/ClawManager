-- No backfill: NULL preserves discovered metadata without asserting capability.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'vision_support_override') = 0,
  'ALTER TABLE llm_models ADD COLUMN vision_support_override BOOLEAN NULL DEFAULT NULL AFTER supports_vision',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
