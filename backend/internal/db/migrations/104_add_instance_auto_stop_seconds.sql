-- NULL retains legacy day settings and the effective defaults of test builds.
-- Exact seconds become authoritative only when an administrator saves them.
ALTER TABLE instance_auto_stop_settings
    ADD COLUMN check_interval_seconds BIGINT UNSIGNED NULL AFTER idle_timeout_days,
    ADD COLUMN idle_timeout_seconds BIGINT UNSIGNED NULL AFTER check_interval_seconds;
