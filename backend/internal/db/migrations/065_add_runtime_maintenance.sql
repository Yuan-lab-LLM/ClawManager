-- Empty by default: no existing runtime is drained or upgraded by this migration.
CREATE TABLE IF NOT EXISTS gateway_runtime_maintenance (
 instance_id INT NOT NULL PRIMARY KEY,
 owner_user_id INT NOT NULL,
 generation INT NOT NULL,
 operation_id CHAR(32) NOT NULL,
 requested_by INT NOT NULL,
 deadline DATETIME(6) NOT NULL,
 cancelled BOOLEAN NOT NULL DEFAULT FALSE,
 gate_closed BOOLEAN NOT NULL DEFAULT FALSE,
 active_executions INT NOT NULL DEFAULT 0,
 active_admissions INT NOT NULL DEFAULT 0,
 active_jobs INT NOT NULL DEFAULT 0,
 queued_inputs INT NOT NULL DEFAULT 0,
 reported_at DATETIME(6) NULL,
 CONSTRAINT fk_runtime_maintenance_instance FOREIGN KEY(instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
