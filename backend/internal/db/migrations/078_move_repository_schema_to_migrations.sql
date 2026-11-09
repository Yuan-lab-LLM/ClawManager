-- Schema formerly created by repository constructors. Existing tables/data remain unchanged.

SET @backup_schema_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='llm_models' AND COLUMN_NAME='protocol_type')=0,
  'ALTER TABLE llm_models ADD COLUMN protocol_type VARCHAR(100) NULL AFTER provider_type', 'SELECT 1');
PREPARE backup_schema_stmt FROM @backup_schema_ddl;
EXECUTE backup_schema_stmt;
DEALLOCATE PREPARE backup_schema_stmt;

UPDATE llm_models SET protocol_type = CASE
  WHEN LOWER(TRIM(provider_type)) = 'openai' THEN 'openai'
  WHEN LOWER(TRIM(provider_type)) = 'anthropic' THEN 'anthropic'
  WHEN LOWER(TRIM(provider_type)) = 'google' THEN 'google'
  WHEN LOWER(TRIM(provider_type)) = 'azure-openai' THEN 'azure-openai'
  ELSE 'openai-compatible' END
WHERE protocol_type IS NULL OR TRIM(protocol_type) = '';

SET @backup_schema_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='model_invocations' AND INDEX_NAME='idx_model_invocations_session_id')=0,
  'ALTER TABLE model_invocations ADD INDEX idx_model_invocations_session_id (session_id)', 'SELECT 1');
PREPARE backup_schema_stmt FROM @backup_schema_ddl;
EXECUTE backup_schema_stmt;
DEALLOCATE PREPARE backup_schema_stmt;

CREATE TABLE IF NOT EXISTS chat_messages (
  id INT AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(100) NOT NULL,
  session_id VARCHAR(100) NOT NULL,
  request_id VARCHAR(100) NULL,
  user_id INT NULL,
  instance_id INT NULL,
  invocation_id INT NULL,
  role VARCHAR(50) NOT NULL,
  content LONGTEXT NOT NULL,
  sequence_no INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_chat_messages_trace_id (trace_id),
  INDEX idx_chat_messages_session_id (session_id),
  INDEX idx_chat_messages_request_id (request_id),
  INDEX idx_chat_messages_user_id (user_id),
  INDEX idx_chat_messages_sequence_no (sequence_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS chat_sessions (
  id INT AUTO_INCREMENT PRIMARY KEY,
  session_id VARCHAR(100) NOT NULL UNIQUE,
  user_id INT NULL,
  instance_id INT NULL,
  title VARCHAR(255) NULL,
  last_trace_id VARCHAR(100) NULL,
  started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_chat_sessions_user_id (user_id),
  INDEX idx_chat_sessions_instance_id (instance_id),
  INDEX idx_chat_sessions_last_activity_at (last_activity_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS risk_rules (
  id INT AUTO_INCREMENT PRIMARY KEY,
  rule_id VARCHAR(100) NOT NULL UNIQUE,
  display_name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  pattern TEXT NOT NULL,
  severity VARCHAR(20) NOT NULL,
  action VARCHAR(50) NOT NULL,
  is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_risk_rules_enabled (is_enabled),
  INDEX idx_risk_rules_sort (sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS security_scan_configs (
			id INT AUTO_INCREMENT PRIMARY KEY,
			default_mode VARCHAR(20) NOT NULL DEFAULT 'quick',
			quick_analyzers_json LONGTEXT NOT NULL,
			deep_analyzers_json LONGTEXT NOT NULL,
			quick_timeout_seconds INT NOT NULL DEFAULT 30,
			deep_timeout_seconds INT NOT NULL DEFAULT 120,
			allow_fallback BOOLEAN NOT NULL DEFAULT TRUE,
			updated_by INT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_security_scan_configs_singleton (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS security_scan_jobs (
			id INT AUTO_INCREMENT PRIMARY KEY,
			asset_type VARCHAR(30) NOT NULL DEFAULT 'skill',
			scan_mode VARCHAR(20) NOT NULL DEFAULT 'quick',
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			requested_by INT NULL,
			scope_json LONGTEXT NULL,
			total_items INT NOT NULL DEFAULT 0,
			completed_items INT NOT NULL DEFAULT 0,
			failed_items INT NOT NULL DEFAULT 0,
			current_item_name VARCHAR(255) NULL,
			started_at TIMESTAMP NULL,
			finished_at TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_security_scan_jobs_status (status, created_at),
			INDEX idx_security_scan_jobs_asset_type (asset_type, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS security_scan_job_items (
			id INT AUTO_INCREMENT PRIMARY KEY,
			job_id INT NOT NULL,
			asset_type VARCHAR(30) NOT NULL DEFAULT 'skill',
			asset_id INT NOT NULL,
			asset_name VARCHAR(255) NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			progress_pct INT NOT NULL DEFAULT 0,
			risk_level VARCHAR(30) NULL,
			summary TEXT NULL,
			scan_result_id INT NULL,
			cached_result BOOLEAN NOT NULL DEFAULT FALSE,
			error_message TEXT NULL,
			started_at TIMESTAMP NULL,
			finished_at TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			FOREIGN KEY (job_id) REFERENCES security_scan_jobs(id) ON DELETE CASCADE,
			UNIQUE KEY uk_security_scan_job_items_job_asset (job_id, asset_type, asset_id),
			INDEX idx_security_scan_job_items_job (job_id, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS security_scan_reports (
			id INT AUTO_INCREMENT PRIMARY KEY,
			job_id INT NOT NULL,
			summary_json LONGTEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			FOREIGN KEY (job_id) REFERENCES security_scan_jobs(id) ON DELETE CASCADE,
			UNIQUE KEY uk_security_scan_reports_job (job_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
