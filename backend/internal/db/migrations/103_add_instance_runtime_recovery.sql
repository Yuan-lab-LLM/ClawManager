CREATE TABLE IF NOT EXISTS instance_runtime_recovery (
 instance_id INT NOT NULL PRIMARY KEY,
 attempts INT NOT NULL DEFAULT 0,
 next_retry_at DATETIME(6) NULL,
 reason TEXT NOT NULL,
 healthy_since DATETIME(6) NULL,
 source_pod_id BIGINT NOT NULL DEFAULT 0,
 source_gateway_port INT NOT NULL DEFAULT 0,
 generation INT NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 CONSTRAINT fk_runtime_recovery_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
