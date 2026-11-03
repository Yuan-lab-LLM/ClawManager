-- Only outstanding platform remote writes. No enrollment or runtime changes.
CREATE TABLE IF NOT EXISTS instance_workspace_write_intents (
 operation_id CHAR(32) NOT NULL PRIMARY KEY,
 instance_id INT NOT NULL,
 owner_user_id INT NOT NULL,
 generation INT NOT NULL,
 action VARCHAR(16) NOT NULL,
 created_at DATETIME(6) NOT NULL,
 KEY idx_workspace_write_instance (instance_id),
 CONSTRAINT fk_workspace_write_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
