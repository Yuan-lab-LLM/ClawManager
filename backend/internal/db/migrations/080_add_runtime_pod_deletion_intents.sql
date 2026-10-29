CREATE TABLE runtime_pod_deletion_intents (
    namespace VARCHAR(128) NOT NULL,
    deployment_name VARCHAR(255) NOT NULL,
    deployment_uid VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (namespace, deployment_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
