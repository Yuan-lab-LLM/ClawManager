-- Audit list rows are durable metadata, independent of body/source retention.
CREATE TABLE IF NOT EXISTS observation_audit_event_summaries (
 id BIGINT PRIMARY KEY, trace_id VARCHAR(100) NOT NULL, request_id VARCHAR(100) NULL,
 session_id VARCHAR(100) NULL,user_id BIGINT NULL,instance_id BIGINT NULL,
 event_type VARCHAR(100) NOT NULL,message TEXT NOT NULL,created_at DATETIME(6) NOT NULL,
 KEY idx_audit_fact_trace(trace_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_audit_trace_reports (
 sequence_id BIGINT NOT NULL AUTO_INCREMENT,trace_id VARCHAR(100) NOT NULL,
 id BIGINT NOT NULL DEFAULT 0,request_id VARCHAR(100) NOT NULL DEFAULT '',session_id VARCHAR(100) NULL,
 user_id BIGINT NULL,instance_id BIGINT NULL,requested_model VARCHAR(255) NOT NULL DEFAULT '',
 actual_provider_model VARCHAR(255) NOT NULL DEFAULT '',provider_type VARCHAR(100) NOT NULL DEFAULT '',status VARCHAR(32) NOT NULL DEFAULT 'pending',
 prompt_tokens BIGINT NOT NULL DEFAULT 0,completion_tokens BIGINT NOT NULL DEFAULT 0,total_tokens BIGINT NOT NULL DEFAULT 0,
 latency_ms BIGINT NULL,ttft_ms INT NULL,generation_ms INT NULL,tokens_per_second DOUBLE NULL,attempt_no INT NOT NULL DEFAULT 0,error_message TEXT NULL,
 created_at DATETIME(6) NULL,completed_at DATETIME(6) NULL,sort_at DATETIME(6) NULL,
 event_type VARCHAR(100) NOT NULL DEFAULT '',message TEXT NOT NULL,
 PRIMARY KEY(trace_id),UNIQUE KEY uk_audit_report_sequence(sequence_id),
 KEY idx_audit_report_time(sort_at,trace_id),KEY idx_audit_report_status_time(status,sort_at,trace_id),
 KEY idx_audit_report_count(sort_at,sequence_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_audit_backfill (
 source VARCHAR(40) PRIMARY KEY,after_id BIGINT NOT NULL DEFAULT 0,target_id BIGINT NOT NULL DEFAULT 0,complete BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT IGNORE INTO observation_audit_backfill(source,updated_at) VALUES
 ('model_invocations',UTC_TIMESTAMP(6)),('observation_invocation_summaries',UTC_TIMESTAMP(6)),
 ('audit_events',UTC_TIMESTAMP(6)),('archive_invocations',UTC_TIMESTAMP(6)),('archive_audits',UTC_TIMESTAMP(6));
UPDATE observation_audit_backfill SET target_id=(SELECT COALESCE(MAX(id),0) FROM model_invocations) WHERE source='model_invocations';
UPDATE observation_audit_backfill SET target_id=(SELECT COALESCE(MAX(id),0) FROM observation_invocation_summaries) WHERE source='observation_invocation_summaries';
UPDATE observation_audit_backfill SET target_id=(SELECT COALESCE(MAX(id),0) FROM audit_events) WHERE source='audit_events';
UPDATE observation_audit_backfill SET target_id=(SELECT COALESCE(MAX(original_id),0) FROM history_archive_records WHERE kind='model_invocations') WHERE source='archive_invocations';
UPDATE observation_audit_backfill SET target_id=(SELECT COALESCE(MAX(original_id),0) FROM history_archive_records WHERE kind='audit_events') WHERE source='archive_audits';
