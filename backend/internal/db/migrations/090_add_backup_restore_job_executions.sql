CREATE TABLE IF NOT EXISTS system_backup_restore_jobs (
 attempt_id VARCHAR(40) PRIMARY KEY,
 task_id VARCHAR(40) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 create_sent BOOLEAN NOT NULL DEFAULT FALSE,
 job_uid VARCHAR(128) NULL,
 stopped_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY uk_sb_restore_job_uid(job_uid),
 FOREIGN KEY(attempt_id) REFERENCES system_backup_attempts(public_id),
 FOREIGN KEY(task_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
