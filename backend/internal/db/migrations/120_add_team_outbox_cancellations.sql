-- A cancelled message was never delivered. Preserve its original business
-- identity separately from delivered receipts, including the deletion proof.
CREATE TABLE IF NOT EXISTS history_outbox_cancellations (
 team_id BIGINT NOT NULL, destination VARCHAR(255) NOT NULL, message_id VARCHAR(255) NOT NULL,
 source_event_id VARCHAR(255) NOT NULL, payload_sha256 CHAR(64) NOT NULL,
 cancelled_at DATETIME(6) NOT NULL, reason VARCHAR(255) NOT NULL, closure_sha256 CHAR(64) NOT NULL,
 PRIMARY KEY(team_id,destination,message_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
ALTER TABLE team_redis_cleanup_jobs ADD COLUMN scan_completed BOOLEAN NOT NULL DEFAULT FALSE;
