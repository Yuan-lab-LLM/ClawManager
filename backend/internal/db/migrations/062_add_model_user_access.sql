SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'visibility_scope') = 0,
  'ALTER TABLE llm_models ADD COLUMN visibility_scope VARCHAR(24) NOT NULL DEFAULT ''all''',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'llm_models' AND COLUMN_NAME = 'access_policy_version') = 0,
  'ALTER TABLE llm_models ADD COLUMN access_policy_version INT NOT NULL DEFAULT 1',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS llm_model_user_grants (
  model_id INT NOT NULL,
  user_id INT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (model_id, user_id),
  INDEX idx_model_user_grants_user (user_id, model_id),
  CONSTRAINT fk_model_user_grants_model FOREIGN KEY (model_id) REFERENCES llm_models(id) ON DELETE CASCADE,
  CONSTRAINT fk_model_user_grants_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
