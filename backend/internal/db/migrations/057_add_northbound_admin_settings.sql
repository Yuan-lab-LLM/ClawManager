CREATE TABLE IF NOT EXISTS northbound_admin_settings (
  id TINYINT PRIMARY KEY,
  api_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  external_node_port INT NOT NULL DEFAULT 0,
  require_explicit_callers BOOLEAN NOT NULL DEFAULT FALSE,
  challenge_ttl_seconds INT NOT NULL DEFAULT 60,
  access_token_ttl_seconds INT NOT NULL DEFAULT 1800,
  refresh_token_ttl_seconds INT NOT NULL DEFAULT 604800,
  core_request_timeout_seconds INT NOT NULL DEFAULT 30,
  challenge_rate_per_minute INT NOT NULL DEFAULT 10,
  login_rate_per_minute INT NOT NULL DEFAULT 5,
  account_login_rate_per_minute INT NOT NULL DEFAULT 5,
  create_rate_per_minute INT NOT NULL DEFAULT 10,
  query_rate_per_minute INT NOT NULL DEFAULT 120,
  share_rate_per_minute INT NOT NULL DEFAULT 10,
  max_pending_operations INT NOT NULL DEFAULT 5,
  operation_tick_milliseconds INT NOT NULL DEFAULT 1000,
  operation_lease_seconds INT NOT NULL DEFAULT 30,
  operation_max_attempts INT NOT NULL DEFAULT 5,
  allowed_lite_types JSON NOT NULL,
  allowed_pro_types JSON NOT NULL,
  lite_cpu_cores DECIMAL(4,2) NOT NULL DEFAULT 2.00,
  lite_memory_gb INT NOT NULL DEFAULT 4,
  lite_disk_gb INT NOT NULL DEFAULT 5,
  pro_cpu_cores DECIMAL(4,2) NOT NULL DEFAULT 4.00,
  pro_memory_gb INT NOT NULL DEFAULT 8,
  pro_disk_gb INT NOT NULL DEFAULT 50,
  workbuddy_pro_cpu_cores DECIMAL(4,2) NOT NULL DEFAULT 4.00,
  workbuddy_pro_memory_gb INT NOT NULL DEFAULT 8,
  workbuddy_pro_disk_gb INT NOT NULL DEFAULT 40,
  version BIGINT NOT NULL DEFAULT 1,
  updated_by INT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT chk_northbound_admin_singleton CHECK (id = 1),
  CONSTRAINT fk_northbound_admin_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO northbound_admin_settings (
  id, allowed_lite_types, allowed_pro_types, created_at, updated_at
) VALUES (
  1,
  JSON_ARRAY('openclaw', 'hermes', 'opencode', 'deepseek-harness', 'workbuddy'),
  JSON_ARRAY('openclaw', 'hermes', 'opencode', 'workbuddy'),
  UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
);

CREATE TABLE IF NOT EXISTS northbound_caller_policies (
  user_id INT PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  scopes_json JSON NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  updated_by INT NULL,
  CONSTRAINT fk_northbound_caller_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_northbound_caller_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS northbound_settings_audit (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  actor_user_id INT NULL,
  action VARCHAR(64) NOT NULL,
  before_json JSON NULL,
  after_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  KEY idx_northbound_settings_audit_created (created_at),
  CONSTRAINT fk_northbound_settings_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
