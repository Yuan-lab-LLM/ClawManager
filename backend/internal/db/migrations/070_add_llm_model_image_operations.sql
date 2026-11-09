-- Additive only: no inference or capability backfill for existing models.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'model_kind') = 0,
  'ALTER TABLE llm_models ADD COLUMN model_kind VARCHAR(32) NOT NULL DEFAULT ''chat''',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'image_generation_enabled') = 0,
  'ALTER TABLE llm_models ADD COLUMN image_generation_enabled BOOLEAN NOT NULL DEFAULT FALSE',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()
   AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'image_edit_enabled') = 0,
  'ALTER TABLE llm_models ADD COLUMN image_edit_enabled BOOLEAN NOT NULL DEFAULT FALSE',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
