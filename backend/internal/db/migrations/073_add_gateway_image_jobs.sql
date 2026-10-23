-- Idempotency tombstones outlive image artifacts. No prompt or image data here.
CREATE TABLE IF NOT EXISTS gateway_image_jobs (
  request_id VARCHAR(64) PRIMARY KEY,
  scope_hash CHAR(64) NOT NULL,
  idempotency_hash CHAR(64) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  user_id INT NOT NULL,
  instance_id INT NULL,
  gateway_key_id BIGINT NULL,
  model_id INT NOT NULL,
  state VARCHAR(16) NOT NULL,
  reservation_id VARCHAR(64) NULL,
  estimate DECIMAL(18,8) NOT NULL,
  charge DECIMAL(18,8) NOT NULL DEFAULT 0,
  settled BOOLEAN NOT NULL DEFAULT FALSE,
  result_metadata LONGTEXT NULL,
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  deadline_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  UNIQUE KEY idx_image_job_idempotency (scope_hash,idempotency_hash),
  KEY idx_image_job_recovery (state,deadline_at),
  KEY idx_image_job_settlement (settled,state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
