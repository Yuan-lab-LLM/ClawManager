CREATE TABLE IF NOT EXISTS system_backup_controller_leases (
 scope VARCHAR(64) PRIMARY KEY,
 owner VARCHAR(128) NOT NULL,
 fencing_token BIGINT UNSIGNED NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_attempts (
 public_id VARCHAR(40) PRIMARY KEY,
 task_id VARCHAR(40) NOT NULL,
 phase VARCHAR(32) NOT NULL,
 attempt_number BIGINT UNSIGNED NOT NULL,
 controller_owner VARCHAR(128) NOT NULL,
 fencing_token BIGINT UNSIGNED NOT NULL,
 status VARCHAR(24) NOT NULL,
 result_json LONGTEXT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 finished_at DATETIME(6) NULL,
 UNIQUE KEY uk_sb_attempt_phase (task_id,phase,attempt_number),
 CONSTRAINT ck_sb_attempt_status CHECK(status IN ('running','succeeded','failed','abandoned')),
 CONSTRAINT ck_sb_attempt_result CHECK(result_json IS NULL OR JSON_VALID(result_json)),
 CONSTRAINT ck_sb_attempt_terminal CHECK((status='running' AND finished_at IS NULL) OR (status<>'running' AND finished_at IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
