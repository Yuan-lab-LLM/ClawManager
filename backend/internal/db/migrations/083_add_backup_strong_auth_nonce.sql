CREATE TABLE IF NOT EXISTS system_backup_strong_auth_nonces (
  nonce_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
  actor_id BIGINT NOT NULL,
  endpoint VARCHAR(160) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  consumed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  INDEX idx_backup_nonce_expiry (expires_at)
) ENGINE=InnoDB;
