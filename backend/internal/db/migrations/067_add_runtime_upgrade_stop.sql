-- Empty by default. No existing instance is stopped or enrolled.
CREATE TABLE IF NOT EXISTS gateway_runtime_upgrade_stops (
 instance_id INT NOT NULL PRIMARY KEY,
 owner_user_id INT NOT NULL,
 generation INT NOT NULL,
 operation_id CHAR(32) NOT NULL,
 runtime_pod_id BIGINT NOT NULL,
 pod_uid VARCHAR(128) NOT NULL,
 agent_epoch CHAR(32) NOT NULL,
 agent_endpoint VARCHAR(1024) NOT NULL,
 gateway_id VARCHAR(100) NOT NULL,
 pid INT NOT NULL,
 confirmation_scope VARCHAR(32) NOT NULL DEFAULT '',
 confirmed_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL,
 CONSTRAINT fk_runtime_upgrade_stop_fence FOREIGN KEY(instance_id) REFERENCES gateway_runtime_upgrades(instance_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
