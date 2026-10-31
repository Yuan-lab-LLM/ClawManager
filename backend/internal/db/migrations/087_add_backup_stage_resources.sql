CREATE TABLE IF NOT EXISTS system_backup_stage_resources (
 public_id VARCHAR(40) PRIMARY KEY,
 task_id VARCHAR(40) NOT NULL,
 phase VARCHAR(32) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 request_json LONGTEXT NOT NULL,
 state VARCHAR(24) NOT NULL DEFAULT 'prepared',
 pvc_create_sent BOOLEAN NOT NULL DEFAULT FALSE,
 pv_create_sent BOOLEAN NOT NULL DEFAULT FALSE,
 pvc_uid VARCHAR(128) NOT NULL DEFAULT '',
 pv_uid VARCHAR(128) NOT NULL DEFAULT '',
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 deleted_at DATETIME(6) NULL,
 UNIQUE KEY uk_sb_stage_task_phase(task_id,phase),
 CONSTRAINT ck_sb_stage_request CHECK(JSON_VALID(request_json)),
 CONSTRAINT ck_sb_stage_phase CHECK(phase IN ('capture','secret_capture')),
 CONSTRAINT ck_sb_stage_state CHECK(state IN ('prepared','ready','delete_requested','deleted')),
 FOREIGN KEY(task_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
