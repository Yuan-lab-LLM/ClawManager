-- A model group is the stable logical name used by Gateway clients. Existing
-- llm_models remain individual provider deployments and are backfilled into
-- one-record groups for full backward compatibility.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'llm_models'
      AND COLUMN_NAME = 'model_group') = 0,
  'ALTER TABLE llm_models ADD COLUMN model_group VARCHAR(100) NOT NULL DEFAULT '''' AFTER protocol_type',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'llm_models'
      AND COLUMN_NAME = 'deployment_priority') = 0,
  'ALTER TABLE llm_models ADD COLUMN deployment_priority INT NOT NULL DEFAULT 100 AFTER model_group',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE llm_models
SET model_group = display_name
WHERE model_group IS NULL OR TRIM(model_group) = '';

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'llm_models'
      AND INDEX_NAME = 'idx_llm_models_group_priority') = 0,
  'ALTER TABLE llm_models ADD INDEX idx_llm_models_group_priority (model_group, deployment_priority, is_active)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
