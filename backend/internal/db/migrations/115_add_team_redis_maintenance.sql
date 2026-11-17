CREATE TABLE IF NOT EXISTS team_outbox_admission (
 id TINYINT PRIMARY KEY, generation BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB;
INSERT IGNORE INTO team_outbox_admission(id) VALUES(1);
CREATE TABLE IF NOT EXISTS team_redis_cleanup_jobs (
 team_id INT PRIMARY KEY, owner_id INT NOT NULL,
 state VARCHAR(32) NOT NULL DEFAULT 'pending', scan_cursor VARCHAR(32) NOT NULL DEFAULT '0',
 producer_ids JSON NOT NULL,
 archived_keys BIGINT NOT NULL DEFAULT 0, deleted_keys BIGINT NOT NULL DEFAULT 0,
 lease_owner VARCHAR(64) NOT NULL DEFAULT '', lease_until DATETIME(6) NULL,
 last_error VARCHAR(1000) NOT NULL DEFAULT '', created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS team_redis_archive_receipts (
 team_id INT NOT NULL, key_sha256 BINARY(32) NOT NULL, key_name VARCHAR(512) NOT NULL,
 chunk_no BIGINT NOT NULL, value_sha256 BINARY(32) NOT NULL, object_ref JSON NOT NULL,
 archived_at DATETIME(6) NOT NULL, deleted_at DATETIME(6) NULL,
 PRIMARY KEY(team_id,key_sha256,chunk_no)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS team_redis_cleanup_keys (
 team_id INT NOT NULL, key_sha256 BINARY(32) NOT NULL, key_name VARCHAR(512) NOT NULL,
 kind VARCHAR(16) NOT NULL, next_cursor VARCHAR(64) NOT NULL DEFAULT '0',
 chunk_no BIGINT NOT NULL DEFAULT 0, archive_complete BOOLEAN NOT NULL DEFAULT FALSE,
 deleted_at DATETIME(6) NULL, PRIMARY KEY(team_id,key_sha256)
) ENGINE=InnoDB;
CREATE INDEX idx_team_outbox_admission ON team_event_outbox(status,team_id,id);
CREATE INDEX idx_team_tasks_admission ON team_tasks(status,team_id,id);
CREATE TABLE IF NOT EXISTS team_redis_dlq_receipts (
 team_id INT NOT NULL, stream_id VARCHAR(128) COLLATE ascii_bin NOT NULL,
 value_sha256 BINARY(32) NOT NULL, object_ref JSON NOT NULL,
 archived_at DATETIME(6) NOT NULL, PRIMARY KEY(team_id,stream_id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS team_projection_command_sources (
 team_id INT NOT NULL, message_id VARCHAR(255) COLLATE utf8mb4_bin NOT NULL,
 source_stream_id VARCHAR(128) COLLATE ascii_bin NOT NULL, task_id INT NOT NULL,
 PRIMARY KEY(team_id,message_id), KEY idx_projection_source_task(team_id,task_id)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS team_completion_receipt_closures (
 team_id INT NOT NULL, event_id BIGINT NOT NULL, task_id INT NOT NULL,
 source_window_sha256 CHAR(64) NOT NULL, closed_at DATETIME(6) NOT NULL,
 redis_reclaimed_at DATETIME(6) NULL, PRIMARY KEY(team_id,event_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS team_outbox_receipt_closures (
 outbox_id BIGINT PRIMARY KEY, team_id BIGINT NOT NULL, task_id BIGINT NOT NULL,
 source_window_sha256 CHAR(64) NOT NULL, closed_before DATETIME(6) NOT NULL, closed_at DATETIME(6) NOT NULL, redis_reclaimed_at DATETIME(6) NULL,
 KEY idx_team_receipt_closure(team_id,outbox_id)
) ENGINE=InnoDB;
