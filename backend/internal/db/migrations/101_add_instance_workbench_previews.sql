-- Preview capabilities grant application access only; they are never writer or
-- container-stop evidence. Store hashes instead of bearer subdomain tokens.
CREATE TABLE IF NOT EXISTS instance_workbench_previews (
 token_hash CHAR(64) NOT NULL PRIMARY KEY,
 instance_id INT NOT NULL,
 user_id INT NOT NULL,
 generation INT NOT NULL,
 gateway_id VARCHAR(96) NOT NULL,
 target_url TEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 KEY idx_workbench_preview_instance (instance_id, generation, expires_at),
 KEY idx_workbench_preview_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
