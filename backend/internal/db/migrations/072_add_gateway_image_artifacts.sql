CREATE TABLE IF NOT EXISTS gateway_image_artifacts (
  id VARCHAR(64) PRIMARY KEY,
  user_id INT NOT NULL,
  instance_id INT NULL,
  gateway_key_id BIGINT NULL,
  model_id INT NOT NULL,
  request_id VARCHAR(100) NOT NULL,
  media_type VARCHAR(32) NOT NULL,
  byte_size BIGINT NOT NULL,
  encryption_key VARBINARY(32) NOT NULL,
  ready BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME NOT NULL,
  expires_at DATETIME NOT NULL,
  INDEX idx_image_artifact_expiry (expires_at),
  INDEX idx_image_artifact_owner (user_id, created_at)
);
