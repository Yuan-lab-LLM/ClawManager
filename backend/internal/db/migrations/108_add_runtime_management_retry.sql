-- Administrator retries are consumed only by the elected scheduler.
-- Queueing does not change an in-flight operation's owner or phase.
ALTER TABLE runtime_management_operations
  ADD COLUMN retry_requested BOOLEAN NOT NULL DEFAULT FALSE;
