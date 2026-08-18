CREATE TABLE IF NOT EXISTS custom_team_templates (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  name VARCHAR(255) NOT NULL,
  intent TEXT NOT NULL,
  requested_member_count INT NULL,
  resolved_member_count INT NOT NULL,
  spec_json LONGTEXT NOT NULL,
  revision INT NOT NULL DEFAULT 1,
  last_trace_id VARCHAR(128) NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  INDEX idx_custom_team_templates_user_name (user_id, name),
  INDEX idx_custom_team_templates_user_updated (user_id, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
