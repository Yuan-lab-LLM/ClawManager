-- No expiry or lease timeout authorizes reopening a closed generation.
CREATE TABLE IF NOT EXISTS instance_workspace_write_fences (
 instance_id INT NOT NULL PRIMARY KEY,
 owner_user_id INT NOT NULL,
 generation INT NOT NULL,
 pod_uid VARCHAR(128) NOT NULL,
 state VARCHAR(16) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 CONSTRAINT fk_workspace_fence_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
