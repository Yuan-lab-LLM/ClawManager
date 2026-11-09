CREATE TABLE IF NOT EXISTS system_backup_dependency_health (
  config_version BIGINT UNSIGNED NOT NULL,
  dependency VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  reason_code VARCHAR(64) NOT NULL,
  evidence_hash CHAR(64) NOT NULL,
  checked_at DATETIME(6) NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
  PRIMARY KEY(config_version,dependency),
  CONSTRAINT ck_sb_dependency_status CHECK (status IN ('healthy','degraded','unknown')),
  CONSTRAINT ck_sb_dependency_reason CHECK ((status='healthy' AND reason_code='none') OR (status<>'healthy' AND reason_code<>'none')),
  CONSTRAINT ck_sb_dependency_expiry CHECK (expires_at>checked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
