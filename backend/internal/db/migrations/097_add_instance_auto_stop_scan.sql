-- A claimed daily scan remains resumable until every selected page is visited.
ALTER TABLE instance_auto_stop_settings
    ADD COLUMN scan_token VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN scan_cursor INT NOT NULL DEFAULT 0,
    ADD COLUMN scan_lease_until DATETIME(6) NULL;
