CREATE TABLE IF NOT EXISTS team_runtime_lifecycle (
  team_id INT PRIMARY KEY,
  state VARCHAR(32) NOT NULL DEFAULT 'running',
  operation_id VARCHAR(64) NOT NULL DEFAULT '',
  desired_state VARCHAR(16) NOT NULL DEFAULT 'running',
  reason VARCHAR(32) NOT NULL DEFAULT '',
  members_json JSON NULL,
  progress INT NOT NULL DEFAULT 0,
  attempts INT NOT NULL DEFAULT 0,
  error_message VARCHAR(512) NOT NULL DEFAULT '',
  observed_since DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  started_at DATETIME(6) NULL,
  stopped_at DATETIME(6) NULL,
  next_attempt_at DATETIME(6) NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  INDEX idx_team_runtime_reconcile(state,next_attempt_at),
  CONSTRAINT fk_team_runtime_team FOREIGN KEY(team_id) REFERENCES teams(id) ON DELETE CASCADE
);
