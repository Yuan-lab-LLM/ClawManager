-- Additive and restart-safe. Existing instance audit records remain unchanged.
CREATE TABLE IF NOT EXISTS team_workspace_file_audits (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 team_id INT NOT NULL,
 user_id INT NOT NULL,
 action VARCHAR(32) NOT NULL,
 relative_path VARCHAR(1024) NOT NULL,
 bytes BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 KEY idx_team_workspace_audit_time (team_id, created_at),
 KEY idx_team_workspace_audit_user (user_id, created_at),
 CONSTRAINT fk_team_workspace_audit_team FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
