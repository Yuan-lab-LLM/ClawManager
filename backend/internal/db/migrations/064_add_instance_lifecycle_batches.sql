-- Core-only task state. Gateway needs no new grants. Old workers ignore
-- batch_pending operations, allowing image rollback without executing queues.
CREATE TABLE IF NOT EXISTS instance_lifecycle_scheduler (
  id INT PRIMARY KEY,
  revision BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB;
INSERT IGNORE INTO instance_lifecycle_scheduler (id) VALUES (1);

-- Direct mutation guards are not automatically retried after process crashes.
CREATE TABLE IF NOT EXISTS instance_lifecycle_manual_guards (
  instance_id INT PRIMARY KEY,
  token VARCHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS instance_lifecycle_batches (
  batch_id VARCHAR(64) PRIMARY KEY,
  user_id INT NOT NULL,
  action VARCHAR(16) NOT NULL,
  status VARCHAR(32) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_lifecycle_batch_user (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS instance_lifecycle_batch_items (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  batch_id VARCHAR(64) NOT NULL,
  source_id INT NOT NULL,
  source_name VARCHAR(255) NOT NULL,
  runtime_type VARCHAR(64) NOT NULL,
  operation_id VARCHAR(64) NOT NULL,
  runtime_pod_id BIGINT NOT NULL DEFAULT 0,
  state VARCHAR(32) NOT NULL DEFAULT 'pending',
  UNIQUE KEY uk_batch_source (batch_id, source_id),
  UNIQUE KEY uk_batch_operation (operation_id),
  KEY idx_batch_state (state, id),
  KEY idx_batch_source_state (source_id, state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
