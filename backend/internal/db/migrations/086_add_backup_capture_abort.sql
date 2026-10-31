CREATE TABLE IF NOT EXISTS system_backup_capture_aborts (
 task_id VARCHAR(40) PRIMARY KEY,
 reason VARCHAR(64) NOT NULL,
 requested_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 FOREIGN KEY(task_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
