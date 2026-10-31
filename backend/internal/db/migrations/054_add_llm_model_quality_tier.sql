-- 0 preserves unclassified legacy models; capability must be set by an admin.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'quality_tier') = 0,
  'ALTER TABLE llm_models ADD COLUMN quality_tier INT NOT NULL DEFAULT 0 AFTER context_window_tokens',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
