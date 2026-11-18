CREATE TABLE IF NOT EXISTS runtime_scale_approvals (
    configuration_digest CHAR(64) CHARACTER SET ascii NOT NULL PRIMARY KEY,
    runtime_type VARCHAR(32) NOT NULL,
    namespace VARCHAR(253) NOT NULL,
    deployment_name VARCHAR(253) NOT NULL,
    deployment_uid VARCHAR(64) NOT NULL,
    source_pod_uid VARCHAR(64) NOT NULL,
    configuration_json JSON NOT NULL,
    approved_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS runtime_scale_startup_baselines (
    deployment_uid VARCHAR(64) NOT NULL,
    attempt_id CHAR(36) CHARACTER SET ascii NOT NULL,
    pod_uids MEDIUMTEXT NOT NULL,
    PRIMARY KEY (deployment_uid, attempt_id)
) ENGINE=InnoDB;
