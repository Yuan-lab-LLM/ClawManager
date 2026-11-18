CREATE TABLE IF NOT EXISTS instance_auto_stop_settings (
    id TINYINT UNSIGNED PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    check_interval_days INT UNSIGNED NOT NULL DEFAULT 1,
    idle_timeout_days INT UNSIGNED NOT NULL DEFAULT 1,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    next_check_at DATETIME(6) NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

INSERT IGNORE INTO instance_auto_stop_settings (id) VALUES (1);

CREATE TABLE IF NOT EXISTS instance_model_activity (
    instance_id INT PRIMARY KEY,
    generation INT NOT NULL,
    monitoring_since DATETIME(6) NOT NULL,
    last_gateway_activity_at DATETIME(6) NULL,
    activity_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    auto_stop_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    stop_operation_id VARCHAR(64) NOT NULL DEFAULT '',
    stop_phase VARCHAR(32) NOT NULL DEFAULT '',
    stopped_reason VARCHAR(32) NOT NULL DEFAULT '',
    stopped_at DATETIME(6) NULL,
    blocker VARCHAR(512) NOT NULL DEFAULT '',
    start_deadline_at DATETIME(6) NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_model_activity_idle (stop_phase, monitoring_since, instance_id),
    CONSTRAINT fk_model_activity_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS instance_model_activity_requests (
    request_id VARCHAR(64) PRIMARY KEY,
    instance_id INT NOT NULL,
    generation INT NOT NULL,
    started_at DATETIME(6) NOT NULL,
    finished_at DATETIME(6) NULL,
    INDEX idx_model_activity_open (instance_id, generation, finished_at),
    CONSTRAINT fk_model_activity_request_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS instance_idle_stop_operations (
    id VARCHAR(64) PRIMARY KEY,
    instance_id INT NOT NULL,
    user_id INT NOT NULL,
    generation INT NOT NULL,
    runtime_pod_id BIGINT NOT NULL,
    gateway_id VARCHAR(128) NOT NULL,
    pod_uid VARCHAR(128) NOT NULL,
    endpoint VARCHAR(512) NOT NULL,
    workspace_path VARCHAR(1024) NOT NULL,
    storage_identity TEXT NOT NULL,
    phase VARCHAR(32) NOT NULL,
    stop_kind VARCHAR(16) NOT NULL DEFAULT 'idle',
    error_message VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    INDEX idx_idle_stop_pending (phase, instance_id),
    CONSTRAINT fk_idle_stop_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);
