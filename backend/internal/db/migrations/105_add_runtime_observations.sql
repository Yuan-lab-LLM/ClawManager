-- Observations are independent of allocation counters and fenced by the Pod UID.
ALTER TABLE runtime_pods
    ADD COLUMN observation_json JSON NULL,
    ADD COLUMN observation_at DATETIME(6) NULL;
