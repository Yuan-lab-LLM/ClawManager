CREATE TABLE IF NOT EXISTS system_backup_artifacts (
 backup_id VARCHAR(40) PRIMARY KEY,
 signed_manifest_json LONGTEXT NOT NULL,
 manifest_hash CHAR(64) NOT NULL,
 upload_request_json LONGTEXT NULL,
 upload_config_create_sent BOOLEAN NOT NULL DEFAULT FALSE,
 upload_config_uid VARCHAR(128) NOT NULL DEFAULT '',
 upload_config_deleted_at DATETIME(6) NULL,
 signed_index_json LONGTEXT NULL,
 cleanup_json LONGTEXT NULL,
 cleanup_hash CHAR(64) NULL,
 artifact_state VARCHAR(24) NOT NULL DEFAULT 'staging',
 artifact_usable BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 CONSTRAINT ck_sb_artifact_manifest CHECK(JSON_VALID(signed_manifest_json)),
 CONSTRAINT ck_sb_artifact_index CHECK(signed_index_json IS NULL OR JSON_VALID(signed_index_json)),
 CONSTRAINT ck_sb_artifact_cleanup CHECK(cleanup_json IS NULL OR JSON_VALID(cleanup_json)),
 CONSTRAINT ck_sb_artifact_state CHECK(artifact_state IN ('staging','committed')),
 FOREIGN KEY(backup_id) REFERENCES system_backup_tasks(public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE system_backup_capture_jobs ADD COLUMN stopped_at DATETIME(6) NULL;

ALTER TABLE system_backup_external_actions DROP CHECK ck_sb_final_action_type,
 ADD CONSTRAINT ck_sb_final_action_type CHECK(action_type IN ('manifest_put','index_put','marker_put','acceptance_put','catalog_registration_append'));
