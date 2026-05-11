-- Initial schema for ClawReef
-- Created: 2026-03-15

-- Users table
CREATE TABLE IF NOT EXISTS users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(255) UNIQUE NOT NULL,
  email VARCHAR(320) UNIQUE NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role ENUM('admin', 'user') DEFAULT 'user',
  is_active BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  last_login TIMESTAMP,
  INDEX idx_username (username),
  INDEX idx_role (role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Instances table
CREATE TABLE IF NOT EXISTS instances (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes') DEFAULT 'ubuntu',
  status ENUM('creating', 'running', 'stopped', 'error', 'deleting') DEFAULT 'creating',
  cpu_cores INT NOT NULL,
  memory_gb INT NOT NULL,
  disk_gb INT NOT NULL,
  gpu_enabled BOOLEAN DEFAULT FALSE,
  gpu_type VARCHAR(100),
  gpu_count INT DEFAULT 0,
  os_type VARCHAR(50) NOT NULL,
  os_version VARCHAR(50) NOT NULL,
  image_registry VARCHAR(255),
  image_tag VARCHAR(100),
  environment_overrides_json LONGTEXT,
  storage_class VARCHAR(50) DEFAULT 'standard',
  mount_path VARCHAR(255) DEFAULT '/data',
  pod_name VARCHAR(255),
  pod_namespace VARCHAR(255),
  pod_ip VARCHAR(45),
  access_url VARCHAR(500),
  access_token VARCHAR(255),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  started_at TIMESTAMP,
  stopped_at TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  INDEX idx_user_id (user_id),
  INDEX idx_status (status),
  INDEX idx_type (type),
  UNIQUE KEY uk_user_instance_name (user_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Persistent volumes table
CREATE TABLE IF NOT EXISTS persistent_volumes (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  pvc_name VARCHAR(255) UNIQUE NOT NULL,
  pvc_namespace VARCHAR(255) NOT NULL,
  storage_size_gb INT NOT NULL,
  storage_class VARCHAR(50),
  mount_path VARCHAR(255),
  status ENUM('pending', 'bound', 'released', 'failed') DEFAULT 'pending',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  INDEX idx_instance_id (instance_id),
  UNIQUE KEY uk_pvc_name_namespace (pvc_name, pvc_namespace)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Backups table
CREATE TABLE IF NOT EXISTS backups (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  backup_name VARCHAR(255) NOT NULL,
  backup_size_gb INT,
  backup_path VARCHAR(500),
  status ENUM('creating', 'completed', 'failed', 'deleted') DEFAULT 'creating',
  backup_type ENUM('manual', 'scheduled') DEFAULT 'manual',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP,
  expires_at TIMESTAMP,
  FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  INDEX idx_instance_id (instance_id),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Backup schedules table
CREATE TABLE IF NOT EXISTS backup_schedules (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  schedule_name VARCHAR(255),
  cron_expression VARCHAR(100) NOT NULL,
  retention_days INT DEFAULT 30,
  is_active BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  INDEX idx_instance_id (instance_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- User quotas table
CREATE TABLE IF NOT EXISTS user_quotas (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL UNIQUE,
  max_instances INT DEFAULT 10,
  max_cpu_cores INT DEFAULT 40,
  max_memory_gb INT DEFAULT 100,
  max_storage_gb INT DEFAULT 500,
  max_gpu_count INT DEFAULT 2,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  INDEX idx_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Instance usage table
CREATE TABLE IF NOT EXISTS instance_usage (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_id INT NOT NULL,
  cpu_usage_percent DECIMAL(5,2),
  memory_usage_gb DECIMAL(10,2),
  disk_usage_gb DECIMAL(10,2),
  gpu_usage_percent DECIMAL(5,2),
  uptime_seconds INT,
  recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  INDEX idx_instance_recorded (instance_id, recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Audit logs table
CREATE TABLE IF NOT EXISTS audit_logs (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT,
  action VARCHAR(100) NOT NULL,
  resource_type VARCHAR(50) NOT NULL,
  resource_id INT,
  details JSON,
  ip_address VARCHAR(45),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
  INDEX idx_user_id (user_id),
  INDEX idx_action (action),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Insert default admin user (password: admin123)
INSERT INTO users (username, email, password_hash, role, is_active)
SELECT 'admin', 'admin@clawreef.local', '$2a$10$pbenze514mwv3pvQySQBVOsF5J4DBXL2kVo1hLa8JFhQu5x3AKvBi', 'admin', TRUE
FROM DUAL
WHERE NOT EXISTS (
  SELECT 1
  FROM users
  WHERE username = 'admin' OR email = 'admin@clawreef.local'
);

-- Insert default quota for admin
INSERT INTO user_quotas (user_id, max_instances, max_cpu_cores, max_memory_gb, max_storage_gb, max_gpu_count)
SELECT users.id, 100, 200, 1000, 5000, 10
FROM users
WHERE username = 'admin'
  AND NOT EXISTS (
    SELECT 1
    FROM user_quotas
    WHERE user_quotas.user_id = users.id
  );
