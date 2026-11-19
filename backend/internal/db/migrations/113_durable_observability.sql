CREATE TABLE IF NOT EXISTS observability_admissions (
 owner VARCHAR(128) NOT NULL,
 request_id VARCHAR(128) NOT NULL,
 state VARCHAR(32) NOT NULL DEFAULT 'executing_or_ambiguous',
 admitted_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 completed_at DATETIME(6) NULL,
 PRIMARY KEY(owner,request_id),
 KEY idx_observability_admission_state(state,admitted_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS observability_objects (
 object_key VARCHAR(255) NOT NULL PRIMARY KEY,
 owner VARCHAR(128) NOT NULL,
 sha256 CHAR(64) NOT NULL,
 byte_length BIGINT NOT NULL,
 encryption_version INT NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 retain_until DATETIME(6) NOT NULL,
 backup_verified_at DATETIME(6) NULL,
 restore_verified_at DATETIME(6) NULL,
 recovery_closed_at DATETIME(6) NULL,
 deleted_at DATETIME(6) NULL,
 KEY idx_observability_object_retention(retain_until,deleted_at)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS observability_body_refs (
 source_table VARCHAR(64) NOT NULL,
 event_uid CHAR(64) NOT NULL,
 field_name VARCHAR(64) NOT NULL,
 object_key VARCHAR(255) NOT NULL,
 body_ref JSON NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 released_at DATETIME(6) NULL,
 PRIMARY KEY(source_table,event_uid,field_name),
 KEY idx_observability_body_object(object_key,released_at)
) ENGINE=InnoDB;
ALTER TABLE model_invocations ADD COLUMN event_uid CHAR(64) NULL, ADD UNIQUE KEY uq_model_invocations_event_uid(event_uid), ADD COLUMN request_body_ref JSON NULL, ADD COLUMN response_body_ref JSON NULL;
ALTER TABLE audit_events ADD COLUMN event_uid CHAR(64) NULL, ADD UNIQUE KEY uq_audit_events_event_uid(event_uid), ADD COLUMN details_body_ref JSON NULL;
ALTER TABLE cost_records ADD COLUMN invocation_event_uid CHAR(64) NULL, ADD COLUMN event_uid CHAR(64) NULL, ADD UNIQUE KEY uq_cost_records_event_uid(event_uid);
ALTER TABLE chat_messages ADD COLUMN event_uid CHAR(64) NULL, ADD UNIQUE KEY uq_chat_messages_event_uid(event_uid), ADD COLUMN content_body_ref JSON NULL;
CREATE TABLE IF NOT EXISTS observability_maintenance_progress (
 source_table VARCHAR(64) NOT NULL PRIMARY KEY,
 last_id BIGINT NOT NULL DEFAULT 0,
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS observability_event_receipts (
 event_uid CHAR(64) NOT NULL PRIMARY KEY,
 source_table VARCHAR(64) NOT NULL,
 original_id BIGINT NOT NULL,
 payload_sha256 CHAR(64) NOT NULL,
 committed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY idx_observability_receipt_source(source_table,original_id)
) ENGINE=InnoDB;
