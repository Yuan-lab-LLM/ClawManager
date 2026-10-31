CREATE TABLE IF NOT EXISTS system_backup_job_sends (
 attempt_id VARCHAR(40) PRIMARY KEY,
 task_id VARCHAR(40) NOT NULL,
 request_hash CHAR(64) NULL,
 sent_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(attempt_id) REFERENCES system_backup_attempts(public_id),
 FOREIGN KEY(task_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Before send intents existed an unbound attempt might already have issued a
-- Create. Preserve that uncertainty across upgrade. NULL denotes a legacy
-- intent whose exact request remains in the immutable attempt row.
INSERT IGNORE INTO system_backup_job_sends(attempt_id,task_id,request_hash)
 SELECT a.public_id,a.task_id,NULL FROM system_backup_attempts a
 JOIN system_backup_tasks t ON t.public_id=a.task_id
 WHERE t.task_type='backup' AND t.status NOT IN ('succeeded','failed','canceled')
 AND a.phase IN ('capture','secret_capture','upload','verify');
