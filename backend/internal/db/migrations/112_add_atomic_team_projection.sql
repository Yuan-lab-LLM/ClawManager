CREATE TABLE IF NOT EXISTS team_projection_receipts (
 team_id INT NOT NULL,
 identity_hash BINARY(32) NOT NULL,
 stream_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(team_id,identity_hash)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS team_projection_audits (
 audit_sha256 CHAR(64) COLLATE ascii_bin NOT NULL PRIMARY KEY,
 team_id INT NOT NULL,
 state_sha256 CHAR(64) COLLATE ascii_bin NOT NULL,
 reviewer VARCHAR(255) NOT NULL,
 manifest_json LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY idx_team_projection_audit(team_id,created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS team_projection_watermarks (
 team_id INT NOT NULL PRIMARY KEY,
 committed_cursor VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 baseline_verified BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;
