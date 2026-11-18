-- No cascading foreign keys: disappearing business/API objects are not stop proof.
CREATE TABLE IF NOT EXISTS team_workspace_write_intents (
 operation_id CHAR(32) NOT NULL PRIMARY KEY,
 team_id INT NOT NULL,
 owner_user_id INT NOT NULL,
 action VARCHAR(16) NOT NULL,
 created_at DATETIME(6) NOT NULL,
 KEY idx_team_workspace_writer (team_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS browser_writer_registry (
 instance_id INT NOT NULL PRIMARY KEY,
 revision BIGINT NOT NULL,
 evidence MEDIUMTEXT NOT NULL,
 updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
