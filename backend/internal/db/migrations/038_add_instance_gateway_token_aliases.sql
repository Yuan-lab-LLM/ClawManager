CREATE TABLE IF NOT EXISTS instance_gateway_token_aliases (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  token_hash CHAR(64) NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  last_used_at TIMESTAMP NULL,
  CONSTRAINT fk_instance_gateway_token_aliases_instance
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  UNIQUE KEY uk_instance_gateway_token_aliases_hash (token_hash),
  INDEX idx_instance_gateway_token_aliases_instance (instance_id),
  INDEX idx_instance_gateway_token_aliases_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;