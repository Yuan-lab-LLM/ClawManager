-- Narrow durable metadata survives payload retention. Stable event IDs deduplicate replay.
CREATE TABLE IF NOT EXISTS observation_invocation_summaries (
 id BIGINT PRIMARY KEY, event_uid VARCHAR(64) NULL, instance_id BIGINT NULL,user_id BIGINT NULL,
 session_id VARCHAR(100) NULL,trace_id VARCHAR(100) NOT NULL,request_id VARCHAR(100) NOT NULL DEFAULT '',
 requested_model VARCHAR(255) NOT NULL DEFAULT '',actual_provider_model VARCHAR(255) NOT NULL DEFAULT '',provider_type VARCHAR(100) NOT NULL DEFAULT '',
 status VARCHAR(32) NOT NULL,prompt_tokens BIGINT NOT NULL DEFAULT 0,completion_tokens BIGINT NOT NULL DEFAULT 0,total_tokens BIGINT NOT NULL DEFAULT 0,
 latency_ms INT NULL,ttft_ms INT NULL,generation_ms INT NULL,tokens_per_second DOUBLE NULL,
 is_streaming BOOLEAN NOT NULL DEFAULT FALSE,attempt_no INT NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL,completed_at DATETIME(6) NULL,error_message TEXT NULL,
 UNIQUE KEY uk_observation_event(event_uid),
 KEY idx_summary_instance_session_time(instance_id,session_id,created_at,id),
 KEY idx_summary_instance_time(instance_id,created_at,id),KEY idx_summary_trace(trace_id,created_at,id),
 KEY idx_summary_time(created_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_daily_summaries (
 scope VARCHAR(16) NOT NULL,instance_id BIGINT NOT NULL,dimension_id VARCHAR(100) NOT NULL,day_utc DATE NOT NULL,
 prompt_tokens BIGINT NOT NULL DEFAULT 0,completion_tokens BIGINT NOT NULL DEFAULT 0,total_tokens BIGINT NOT NULL DEFAULT 0,
 invocation_count BIGINT NOT NULL DEFAULT 0,first_seen_at DATETIME(6) NOT NULL,last_seen_at DATETIME(6) NOT NULL,
 PRIMARY KEY(scope,instance_id,dimension_id,day_utc), KEY idx_summary_day(scope,day_utc,instance_id,dimension_id), KEY idx_summary_rebuild_day(day_utc,scope,instance_id,dimension_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_archive_receipts (
 kind VARCHAR(32) NOT NULL,original_id BIGINT NOT NULL,payload_sha256 CHAR(64) NOT NULL,metadata_json JSON NOT NULL,
 PRIMARY KEY(kind,original_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_summary_state (
 id INT PRIMARY KEY,target_id BIGINT NOT NULL,ready BOOLEAN NOT NULL DEFAULT FALSE
) ENGINE=InnoDB;
INSERT IGNORE INTO observation_summary_state(id,target_id,ready) SELECT 1,COALESCE(MAX(id),0),COALESCE(MAX(id),0)=0 FROM model_invocations;
