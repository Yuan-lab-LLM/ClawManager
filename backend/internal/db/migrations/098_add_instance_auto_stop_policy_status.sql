-- Live default-policy status is display metadata and must not gate idle stop.
CREATE TABLE IF NOT EXISTS instance_auto_stop_policy_status (
    instance_id INT PRIMARY KEY,
    generation INT NOT NULL,
    pod_uid VARCHAR(128) NOT NULL,
    defaults_policy_version VARCHAR(64) NOT NULL DEFAULT '',
    defaults_policy_status VARCHAR(64) NOT NULL DEFAULT 'unknown',
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT fk_auto_stop_policy_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);
