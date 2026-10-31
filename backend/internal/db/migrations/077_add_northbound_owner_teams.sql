-- Additive and restartable: no owner inference, historical renames or data deletion.

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='teams' AND COLUMN_NAME='resource_scope')=0,
  'ALTER TABLE teams ADD COLUMN resource_scope VARCHAR(16) NOT NULL DEFAULT ''cm''', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='teams' AND COLUMN_NAME='owner')=0,
  'ALTER TABLE teams ADD COLUMN owner VARCHAR(128) COLLATE utf8mb4_bin NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='teams' AND COLUMN_NAME='display_name')=0,
  'ALTER TABLE teams ADD COLUMN display_name VARCHAR(255) NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='teams' AND INDEX_NAME='idx_teams_owner_scope')=0,
  'ALTER TABLE teams ADD INDEX idx_teams_owner_scope (resource_scope, owner, id)', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='teams' AND INDEX_NAME='idx_teams_provider_owner')=0,
  'ALTER TABLE teams ADD INDEX idx_teams_provider_owner (user_id, resource_scope, owner, id)', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='custom_team_templates' AND COLUMN_NAME='resource_scope')=0,
  'ALTER TABLE custom_team_templates ADD COLUMN resource_scope VARCHAR(16) NOT NULL DEFAULT ''cm''', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='custom_team_templates' AND COLUMN_NAME='owner')=0,
  'ALTER TABLE custom_team_templates ADD COLUMN owner VARCHAR(128) COLLATE utf8mb4_bin NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='custom_team_templates' AND COLUMN_NAME='last_operation_id')=0,
  'ALTER TABLE custom_team_templates ADD COLUMN last_operation_id VARCHAR(128) NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='custom_team_templates' AND INDEX_NAME='idx_team_templates_owner')=0,
  'ALTER TABLE custom_team_templates ADD INDEX idx_team_templates_owner (resource_scope, owner, user_id, id)', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND COLUMN_NAME='owner')=0,
  'ALTER TABLE northbound_operations ADD COLUMN owner VARCHAR(128) COLLATE utf8mb4_bin NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND COLUMN_NAME='team_id')=0,
  'ALTER TABLE northbound_operations ADD COLUMN team_id INT NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND COLUMN_NAME='template_id')=0,
  'ALTER TABLE northbound_operations ADD COLUMN template_id INT NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND INDEX_NAME='idx_nb_operations_team')=0,
  'ALTER TABLE northbound_operations ADD INDEX idx_nb_operations_team (team_id, status)', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND INDEX_NAME='idx_nb_operations_owner')=0,
  'ALTER TABLE northbound_operations ADD INDEX idx_nb_operations_owner (owner, operation_id)', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_admin_settings' AND COLUMN_NAME='owner_teams_enabled')=0,
  'ALTER TABLE northbound_admin_settings ADD COLUMN owner_teams_enabled BOOLEAN NOT NULL DEFAULT FALSE', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_admin_settings' AND COLUMN_NAME='owner_team_creation_user_id')=0,
  'ALTER TABLE northbound_admin_settings ADD COLUMN owner_team_creation_user_id INT NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

CREATE TABLE IF NOT EXISTS owner_team_provision_steps (
  team_id INT NOT NULL,
  step_key VARCHAR(100) NOT NULL,
  operation_id VARCHAR(128) NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'pending',
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (team_id, step_key),
  FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS owner_team_admission_locks (
  team_id INT NOT NULL PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS iei_session_revocations (
  session_hash CHAR(64) NOT NULL PRIMARY KEY,
  expires_at DATETIME NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND COLUMN_NAME='owner_request_key')=0,
  'ALTER TABLE northbound_operations ADD COLUMN owner_request_key CHAR(64) NULL', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;

SET @owner_team_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND INDEX_NAME='uk_owner_team_request')=0,
  'ALTER TABLE northbound_operations ADD UNIQUE KEY uk_owner_team_request (owner_request_key)', 'SELECT 1');
PREPARE owner_team_stmt FROM @owner_team_ddl;
EXECUTE owner_team_stmt;
DEALLOCATE PREPARE owner_team_stmt;
