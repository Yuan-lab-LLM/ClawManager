CREATE TABLE IF NOT EXISTS runtime_slot_reset_requests (
 request_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 operation_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 pod_uid VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 slot VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 source_pod_id BIGINT NOT NULL DEFAULT 0,
 gateway_port INT NOT NULL DEFAULT 0,
 instance_id INT NOT NULL DEFAULT 0,
 source_generation INT NOT NULL DEFAULT 0,
 recovery_generation INT NOT NULL DEFAULT 0,
 source_gateway_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
 active TINYINT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL,
 UNIQUE KEY uq_slot_reset_operation (operation_id),
 UNIQUE KEY uq_active_slot_reset (pod_uid,slot,active),
 KEY idx_slot_reset_instance_generation (instance_id,source_generation,created_at),
 KEY idx_slot_reset_recovery (instance_id,recovery_generation,created_at),
 KEY idx_slot_reset_admission (source_pod_id,gateway_port,active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
