SET @instance_skills_workspace_dir_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'instance_skills'
    AND COLUMN_NAME = 'workspace_dir'
);
SET @instance_skills_workspace_dir_sql = IF(
  @instance_skills_workspace_dir_exists = 0,
  'ALTER TABLE instance_skills ADD COLUMN workspace_dir VARCHAR(120) NULL AFTER install_path',
  'SELECT 1'
);
PREPARE instance_skills_workspace_dir_stmt FROM @instance_skills_workspace_dir_sql;
EXECUTE instance_skills_workspace_dir_stmt;
DEALLOCATE PREPARE instance_skills_workspace_dir_stmt;

CREATE TABLE IF NOT EXISTS skill_package_materialize_jobs (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  skill_id INT NOT NULL,
  blob_id INT NOT NULL,
  workspace_dir VARCHAR(120) NOT NULL,
  content_hash VARCHAR(128) NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'pending',
  attempt_count INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 5,
  last_error TEXT NULL,
  idempotency_key VARCHAR(255) NOT NULL,
  trigger_source VARCHAR(50) NOT NULL DEFAULT 'sync',
  started_at TIMESTAMP NULL,
  finished_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  FOREIGN KEY (skill_id) REFERENCES skills(id) ON DELETE CASCADE,
  FOREIGN KEY (blob_id) REFERENCES skill_blobs(id) ON DELETE CASCADE,
  UNIQUE KEY uk_sp_materialize_idempotency (idempotency_key),
  INDEX idx_sp_materialize_status_created (status, created_at),
  INDEX idx_sp_materialize_instance_status (instance_id, status),
  INDEX idx_sp_materialize_blob (blob_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

UPDATE instance_skills
SET workspace_dir = SUBSTRING_INDEX(REPLACE(install_path, '\\', '/'), '/', -1)
WHERE workspace_dir IS NULL
  AND install_path IS NOT NULL
  AND TRIM(install_path) <> '';

UPDATE instance_commands ic
JOIN instances i ON i.id = ic.instance_id
SET ic.status = 'cancelled',
    ic.error_message = 'superseded by skill_package_materialize_jobs'
WHERE ic.command_type = 'collect_skill_package'
  AND ic.status IN ('pending', 'dispatched', 'running')
  AND (LOWER(TRIM(i.instance_mode)) = 'lite' OR LOWER(TRIM(i.runtime_type)) = 'gateway');

SET @instance_skills_workspace_dir_index_exists = (
  SELECT COUNT(*)
  FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'instance_skills'
    AND INDEX_NAME = 'idx_instance_skills_workspace_dir'
);
SET @instance_skills_workspace_dir_index_sql = IF(
  @instance_skills_workspace_dir_index_exists = 0,
  'ALTER TABLE instance_skills ADD INDEX idx_instance_skills_workspace_dir (workspace_dir)',
  'SELECT 1'
);
PREPARE instance_skills_workspace_dir_index_stmt FROM @instance_skills_workspace_dir_index_sql;
EXECUTE instance_skills_workspace_dir_index_stmt;
DEALLOCATE PREPARE instance_skills_workspace_dir_index_stmt;
