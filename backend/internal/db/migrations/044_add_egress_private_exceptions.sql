CREATE TABLE IF NOT EXISTS egress_private_exceptions (
  id INT AUTO_INCREMENT PRIMARY KEY,
  scope_type VARCHAR(20) NOT NULL,
  scope_id INT NOT NULL,
  cidr VARCHAR(64) NOT NULL,
  port INT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  description TEXT NULL,
  created_by INT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_egress_private_exceptions_scope (scope_type, scope_id),
  INDEX idx_egress_private_exceptions_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
