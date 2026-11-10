ALTER TABLE instance_model_activity_requests
    ADD COLUMN lease_expires_at DATETIME(6) NULL,
    ADD COLUMN last_heartbeat_at DATETIME(6) NULL,
    ADD COLUMN completion_reason VARCHAR(32) NOT NULL DEFAULT '',
    ADD INDEX idx_model_activity_lease (finished_at, lease_expires_at, instance_id);
