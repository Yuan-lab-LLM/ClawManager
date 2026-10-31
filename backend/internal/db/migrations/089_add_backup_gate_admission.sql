CREATE TABLE IF NOT EXISTS system_backup_gate_admissions (
 scope VARCHAR(128) PRIMARY KEY,
 owner VARCHAR(128) NULL,
 close_until DATETIME(6) NULL,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO system_backup_gate_admissions(scope)
 SELECT scope FROM system_backup_gate;
