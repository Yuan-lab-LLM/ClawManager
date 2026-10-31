CREATE TABLE IF NOT EXISTS system_backup_capture_plans (
 task_id VARCHAR(40) PRIMARY KEY,
 scope VARCHAR(128) NOT NULL,
 generation BIGINT UNSIGNED NOT NULL,
 fencing_token BIGINT UNSIGNED NOT NULL,
 request_hash CHAR(64) NOT NULL,
 request_json LONGTEXT NOT NULL,
 manifest_json LONGTEXT NULL,
 seal_json LONGTEXT NULL,
 source_resumed_at DATETIME(6) NULL,
 forced_resume_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY uk_sb_capture_generation(scope,generation),
 CONSTRAINT ck_sb_capture_plan_json CHECK(JSON_VALID(request_json)),
 FOREIGN KEY(task_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_capture_jobs (
 attempt_id VARCHAR(40) PRIMARY KEY,
 task_id VARCHAR(40) NOT NULL,
 namespace_name VARCHAR(253) NOT NULL,
 job_name VARCHAR(253) NOT NULL,
 job_uid VARCHAR(128) NOT NULL,
 ready BOOLEAN NOT NULL DEFAULT FALSE,
 observed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY uk_sb_capture_job_uid(job_uid),
 FOREIGN KEY(attempt_id) REFERENCES system_backup_attempts(public_id),
 FOREIGN KEY(task_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- One atomic DDL statement retains a constraint throughout crash/retry.
ALTER TABLE system_backup_attempts DROP CHECK ck_sb_attempt_status,
 ADD CONSTRAINT ck_sb_attempt_status CHECK(status IN ('running','sealed','succeeded','failed','abandoned'));
