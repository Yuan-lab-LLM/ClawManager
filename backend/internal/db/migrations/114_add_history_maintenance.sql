-- Maintenance progress and archive evidence are durable business metadata.
CREATE TABLE IF NOT EXISTS history_maintenance_leases (
 name VARCHAR(64) PRIMARY KEY, holder VARCHAR(100) NOT NULL DEFAULT '', lease_until DATETIME(6) NOT NULL,
 last_started_at DATETIME(6) NULL, last_finished_at DATETIME(6) NULL, processed_rows INT NOT NULL DEFAULT 0,
 last_error VARCHAR(1000) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS history_month_catalog (
 table_name VARCHAR(64) PRIMARY KEY, kind VARCHAR(32) NOT NULL, month_utc CHAR(6) NOT NULL,
 schema_version INT NOT NULL, schema_sha256 CHAR(64) NOT NULL, state VARCHAR(20) NOT NULL DEFAULT 'prepared',
 created_at DATETIME(6) NOT NULL, verified_at DATETIME(6) NULL, verify_after_id BIGINT NOT NULL DEFAULT 0, revision BIGINT NOT NULL DEFAULT 0,
 read_route VARCHAR(16) NOT NULL DEFAULT 'legacy', route_after_id BIGINT NOT NULL DEFAULT 0,
 route_verified BOOLEAN NOT NULL DEFAULT FALSE,
 UNIQUE KEY uk_history_kind_month(kind,month_utc)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS history_archive_records (
 kind VARCHAR(32) NOT NULL, original_id BIGINT NOT NULL, owner_id BIGINT NOT NULL,
 instance_id BIGINT NOT NULL DEFAULT 0, team_id BIGINT NOT NULL DEFAULT 0,
 trace_id VARCHAR(100) NOT NULL DEFAULT '', session_id VARCHAR(100) NOT NULL DEFAULT '',
 created_at DATETIME(6) NOT NULL, month_table VARCHAR(64) NOT NULL,
 object_ref JSON NOT NULL, payload_sha256 CHAR(64) NOT NULL, payload_bytes BIGINT NOT NULL,
 metadata_json JSON NOT NULL, summary_verified_at DATETIME(6) NULL, summary_sha256 CHAR(64) NOT NULL DEFAULT '',
 archived_at DATETIME(6) NOT NULL, readback_verified_at DATETIME(6) NOT NULL,
 restore_verified_at DATETIME(6) NULL, restore_sha256 CHAR(64) NOT NULL DEFAULT '',
 restore_receipt VARCHAR(255) NOT NULL DEFAULT '', source_deleted_at DATETIME(6) NULL,
 PRIMARY KEY(kind,original_id), KEY idx_history_owner_time(owner_id,created_at,original_id),
 KEY idx_history_trace(owner_id,trace_id,created_at,original_id),
 KEY idx_history_instance(owner_id,instance_id,created_at,original_id),
 KEY idx_history_retention(kind,created_at,source_deleted_at,original_id),
 KEY idx_history_summary_verify(summary_verified_at,kind,original_id),
 KEY idx_history_month(month_table,original_id),
 KEY idx_history_kind_trace(kind,trace_id,original_id),
 KEY idx_history_kind_session(kind,session_id,original_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- Monthly objects serve canonical typed online records and archive references.
-- Large bodies remain encrypted within owner scope. These tables share the
-- same MySQL I/O failure domain; fixed native sources remain rollback copies.
CREATE TABLE IF NOT EXISTS history_month_template (
 original_id BIGINT PRIMARY KEY, owner_id BIGINT NOT NULL, created_at DATETIME(6) NOT NULL,
 trace_id VARCHAR(100) NOT NULL DEFAULT '', instance_id BIGINT NOT NULL DEFAULT 0,
 object_ref JSON NOT NULL, payload_sha256 CHAR(64) NOT NULL, payload_bytes BIGINT NOT NULL,
 session_id VARCHAR(100) NOT NULL DEFAULT '', team_id BIGINT NOT NULL DEFAULT 0,
 metadata_json JSON NULL,
 -- Preserve canonical typed JSON bytes: MySQL JSON rewrites DECIMAL lexemes.
 payload_json LONGTEXT NULL,
 KEY idx_history_month_owner(owner_id,created_at,original_id),
 KEY idx_history_month_trace(owner_id,trace_id,created_at,original_id),
 KEY idx_history_month_public_trace(trace_id,original_id), KEY idx_history_month_session(session_id,original_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- Stable public ID locator only. Serving payloads and metadata live in months.
CREATE TABLE IF NOT EXISTS history_log_locations (
 kind VARCHAR(32) NOT NULL, original_id BIGINT NOT NULL, month_table VARCHAR(64) NOT NULL,
 PRIMARY KEY(kind,original_id), KEY idx_history_log_month(month_table,original_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- Source-window closure is explicit; absent/ambiguous closure fails closed.
CREATE TABLE IF NOT EXISTS history_replay_closures (
 team_id BIGINT PRIMARY KEY, closed_before DATETIME(6) NOT NULL,
 replay_closed BOOLEAN NOT NULL DEFAULT FALSE, recovery_closed BOOLEAN NOT NULL DEFAULT FALSE,
 idempotency_durable BOOLEAN NOT NULL DEFAULT FALSE, proof_sha256 CHAR(64) NOT NULL,
 verified_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- Kept when delivered outbox payload is removed, so a delayed enqueue cannot
-- recreate previously delivered message identity. Not a time-expiring cache.
CREATE TABLE IF NOT EXISTS history_outbox_receipts (
 team_id BIGINT NOT NULL, destination VARCHAR(255) NOT NULL, message_id VARCHAR(255) NOT NULL,
 source_event_id VARCHAR(255) NOT NULL, payload_sha256 CHAR(64) NOT NULL,
 delivered_at DATETIME(6) NOT NULL, closure_sha256 CHAR(64) NOT NULL,
 PRIMARY KEY(team_id,destination,message_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS history_maintenance_cursors (
 kind VARCHAR(32) PRIMARY KEY, after_id BIGINT NOT NULL DEFAULT 0, updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Permanent business idempotency survives removal of archived Team payloads.
CREATE TABLE IF NOT EXISTS history_identity_receipts (
 kind VARCHAR(32) NOT NULL, team_id BIGINT NOT NULL, key_type VARCHAR(32) NOT NULL,
 key_hash BINARY(32) NOT NULL, original_id BIGINT NOT NULL, payload_sha256 CHAR(64) NOT NULL,
 PRIMARY KEY(kind,team_id,key_type,key_hash), KEY idx_history_identity_record(kind,original_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
