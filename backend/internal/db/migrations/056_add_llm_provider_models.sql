SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'llm_models'
      AND COLUMN_NAME = 'provider_models_json') = 0,
  'ALTER TABLE llm_models ADD COLUMN provider_models_json TEXT NULL AFTER provider_model_name',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
