-- Diagnostic history only. No backfill, enrollment or state mutation.
CREATE TABLE IF NOT EXISTS runtime_shutdown_audits (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  owner_user_id INT NOT NULL,
  generation INT NOT NULL,
  claim_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  outcome VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  source VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  received_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_runtime_shutdown_observation (instance_id,owner_user_id,generation,claim_id,outcome,source),
  KEY ix_runtime_shutdown_history (instance_id,owner_user_id,id),
  CONSTRAINT fk_runtime_shutdown_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
