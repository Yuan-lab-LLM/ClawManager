-- Reconcile installations that have already applied an older runtime migration.
-- Keep every currently supported value whenever the MySQL ENUM is rewritten.
ALTER TABLE instances
MODIFY COLUMN type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes', 'workbuddy', 'opencode', 'deepseek-harness', 'codex', 'claude-code') DEFAULT 'ubuntu';
