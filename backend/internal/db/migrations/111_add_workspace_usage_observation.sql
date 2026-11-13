-- Legacy byte values lack collection proof and remain unavailable until sampled.
-- The reset flag covers the erasure window even while status remains stopped.
ALTER TABLE instances
 ADD COLUMN workspace_usage_observed_at DATETIME(6) NULL,
 ADD COLUMN workspace_usage_epoch BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN workspace_usage_blocked BOOLEAN NOT NULL DEFAULT FALSE;
