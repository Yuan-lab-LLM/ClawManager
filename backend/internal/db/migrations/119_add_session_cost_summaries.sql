-- SQL cost_records remains the immutable ledger. These rebuildable projections
-- deduplicate ledger IDs and preserve exact versus legacy trace association.
CREATE TABLE IF NOT EXISTS observation_cost_facts (
 id BIGINT PRIMARY KEY,invocation_id BIGINT NULL,instance_id BIGINT NULL,session_id VARCHAR(100) NULL,
 trace_id VARCHAR(100) NOT NULL,currency VARCHAR(16) NOT NULL,estimated_cost DECIMAL(24,8) NOT NULL,
 eligible_at DATETIME(6) NULL,resolved BOOLEAN NOT NULL DEFAULT FALSE,
 KEY idx_cost_fact_pending(resolved,id),KEY idx_cost_fact_invocation(invocation_id,resolved,id),
 KEY idx_cost_fact_boundary(instance_id,session_id,eligible_at,id), KEY idx_cost_fact_time(eligible_at,instance_id,session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_session_cost_daily (
 instance_id BIGINT NOT NULL,session_id VARCHAR(100) NOT NULL,day_utc DATE NOT NULL,currency VARCHAR(16) NOT NULL,
 estimated_cost DECIMAL(24,8) NOT NULL,
 PRIMARY KEY(instance_id,session_id,day_utc,currency),KEY idx_session_cost_day(day_utc,instance_id,session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS observation_legacy_trace_costs (
 instance_id BIGINT NOT NULL,session_id VARCHAR(100) NOT NULL,trace_id VARCHAR(100) NOT NULL,currency VARCHAR(16) NOT NULL,
 estimated_cost DECIMAL(24,8) NOT NULL,
 PRIMARY KEY(instance_id,session_id,trace_id,currency)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT IGNORE INTO observation_summary_state(id,target_id,ready) SELECT 2,COALESCE(MAX(id),0),COALESCE(MAX(id),0)=0 FROM cost_records;
CREATE TABLE IF NOT EXISTS observation_cost_overview_daily (
 scope VARCHAR(16) NOT NULL,dimension_id VARCHAR(255) NOT NULL,day_utc DATE NOT NULL,currency VARCHAR(16) NOT NULL,
 record_count BIGINT NOT NULL,prompt_tokens BIGINT NOT NULL,completion_tokens BIGINT NOT NULL,total_tokens BIGINT NOT NULL,
 estimated_cost DECIMAL(24,8) NOT NULL,internal_cost DECIMAL(24,8) NOT NULL,
 PRIMARY KEY(scope,dimension_id,day_utc,currency),KEY idx_cost_overview_day(day_utc,scope)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
