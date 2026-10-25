CREATE TABLE IF NOT EXISTS northbound_auth_challenges (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  challenge_id VARCHAR(64) NOT NULL,
  nonce_hash CHAR(64) NOT NULL,
  key_id VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'issued',
  source_ip VARCHAR(64) NULL,
  expires_at DATETIME(6) NOT NULL,
  used_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_northbound_challenge_id (challenge_id),
  KEY idx_northbound_challenge_expiry (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS northbound_sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id VARCHAR(64) NOT NULL,
  user_id INT NOT NULL,
  refresh_token_hash CHAR(64) NOT NULL,
  previous_refresh_token_hash CHAR(64) NULL,
  refresh_token_history JSON NOT NULL,
  scopes_json JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  access_expires_at DATETIME(6) NOT NULL,
  refresh_expires_at DATETIME(6) NOT NULL,
  last_used_at DATETIME(6) NULL,
  last_ip VARCHAR(64) NULL,
  user_agent VARCHAR(512) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  revoked_at DATETIME(6) NULL,
  UNIQUE KEY uk_northbound_session_id (session_id),
  UNIQUE KEY uk_northbound_refresh_hash (refresh_token_hash),
  KEY idx_northbound_previous_refresh_hash (previous_refresh_token_hash),
  KEY idx_northbound_session_user_status (user_id, status),
  KEY idx_northbound_session_expiry (status, refresh_expires_at),
  CONSTRAINT fk_northbound_session_user
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS northbound_operations (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  operation_id VARCHAR(64) NOT NULL,
  user_id INT NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  operation_type VARCHAR(64) NOT NULL,
  idempotency_key_hash CHAR(64) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  request_payload JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  available_at DATETIME(6) NOT NULL,
  instance_id INT NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  lease_owner VARCHAR(128) NULL,
  lease_expires_at DATETIME(6) NULL,
  error_code VARCHAR(64) NULL,
  error_message VARCHAR(512) NULL,
  created_at DATETIME(6) NOT NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_northbound_operation_id (operation_id),
  UNIQUE KEY uk_northbound_operation_idempotency (user_id, operation_type, idempotency_key_hash),
  KEY idx_northbound_operation_claim (status, available_at, lease_expires_at, id),
  KEY idx_northbound_operation_user (user_id, created_at),
  CONSTRAINT fk_northbound_operation_user
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_northbound_operation_instance
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'instances' AND COLUMN_NAME = 'provisioning_operation_id') = 0,
  'ALTER TABLE instances ADD COLUMN provisioning_operation_id VARCHAR(64) NULL AFTER runtime_error_message',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'instances' AND INDEX_NAME = 'uk_instances_provisioning_operation') = 0,
  'ALTER TABLE instances ADD UNIQUE KEY uk_instances_provisioning_operation (provisioning_operation_id)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
