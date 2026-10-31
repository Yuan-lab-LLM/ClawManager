ALTER TABLE runtime_pods ADD COLUMN scheduling_paused BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE runtime_management_operations (
 id VARCHAR(64) PRIMARY KEY,
 kind VARCHAR(24) NOT NULL,
 status VARCHAR(24) NOT NULL,
 actor_user_id INT NULL,
 request_json LONGTEXT NOT NULL,
 items_json LONGTEXT NOT NULL,
 cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
 owner_token VARCHAR(64) NOT NULL DEFAULT '',
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 KEY idx_runtime_management_status (status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE runtime_management_admission_lock (
 id INT PRIMARY KEY,
 revision BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB;
INSERT INTO runtime_management_admission_lock (id) VALUES (1);
