-- llm_models predates the embedded migration system and was historically
-- created by llmModelRepository.ensureTable.  A fresh installation therefore
-- reaches this migration before the repository has had a chance to create the
-- table.  Preserve that legacy upgrade path while making clean installs and
-- interrupted migration retries safe.
CREATE TABLE IF NOT EXISTS llm_models (
  id INT AUTO_INCREMENT PRIMARY KEY,
  display_name VARCHAR(255) NOT NULL UNIQUE,
  description TEXT NULL,
  provider_type VARCHAR(100) NOT NULL,
  protocol_type VARCHAR(100) NULL,
  base_url VARCHAR(500) NOT NULL,
  provider_model_name VARCHAR(255) NOT NULL,
  api_key TEXT NULL,
  api_key_secret_ref VARCHAR(255) NULL,
  is_secure BOOLEAN NOT NULL DEFAULT FALSE,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  input_price DECIMAL(18,8) NOT NULL DEFAULT 0,
  output_price DECIMAL(18,8) NOT NULL DEFAULT 0,
  currency VARCHAR(16) NOT NULL DEFAULT 'USD',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_llm_models_provider_type (provider_type),
  INDEX idx_llm_models_is_active (is_active),
  INDEX idx_llm_models_is_secure (is_secure)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'llm_models'
      AND COLUMN_NAME = 'reasoning_enabled') = 0,
  'ALTER TABLE llm_models ADD COLUMN reasoning_enabled BOOLEAN NOT NULL DEFAULT FALSE AFTER provider_model_name',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
