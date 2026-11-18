CREATE TABLE IF NOT EXISTS runtime_failover_intents (
 instance_id INT NOT NULL PRIMARY KEY,
 user_id INT NOT NULL,
 generation INT NOT NULL,
 runtime_type VARCHAR(32) NOT NULL,
 source_pod_id BIGINT NOT NULL,
 source_pod_uid VARCHAR(255) NOT NULL,
 source_endpoint VARCHAR(512) NOT NULL,
 gateway_id VARCHAR(128) NOT NULL,
 gateway_port INT NOT NULL,
 workspace_path TEXT NOT NULL,
 reason TEXT NOT NULL,
 attempts INT NOT NULL DEFAULT 0,
 next_retry_at DATETIME(6) NULL,
 source_stopped BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at DATETIME(6) NOT NULL,
 INDEX idx_runtime_failover_due (source_stopped, attempts, next_retry_at, instance_id),
 CONSTRAINT fk_runtime_failover_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
