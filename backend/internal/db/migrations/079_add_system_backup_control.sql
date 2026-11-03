-- Backup control state is installation-local and excluded from business dumps.
CREATE TABLE IF NOT EXISTS system_backup_installation_state (
  singleton_id TINYINT PRIMARY KEY,
  origin_installation_id VARCHAR(64) NOT NULL,
  config_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
  controller_seen_at DATETIME(6) NULL,
  CONSTRAINT ck_sb_singleton CHECK (singleton_id=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_config_versions (
  version BIGINT UNSIGNED PRIMARY KEY,
  config_json LONGTEXT NOT NULL,
  actor_id INT NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  CONSTRAINT ck_sb_config_json CHECK (JSON_VALID(config_json))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_tasks (
  public_id VARCHAR(40) PRIMARY KEY,
  origin_installation_id VARCHAR(64) NOT NULL,
  task_type VARCHAR(24) NOT NULL,
  purpose VARCHAR(24) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
  config_version BIGINT UNSIGNED NOT NULL,
  config_snapshot LONGTEXT NOT NULL,
  request_json LONGTEXT NOT NULL,
  actor_id INT NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  source_public_id VARCHAR(40) NOT NULL DEFAULT '',
  can_cancel BOOLEAN NOT NULL DEFAULT TRUE,
  commit_started BOOLEAN NOT NULL DEFAULT FALSE,
  artifact_registered BOOLEAN NOT NULL DEFAULT FALSE,
  acceptance_eligible BOOLEAN NOT NULL DEFAULT FALSE,
  failure_category VARCHAR(64) NOT NULL DEFAULT 'none',
  pending_reason VARCHAR(40) NOT NULL DEFAULT '',
  attention_reason VARCHAR(40) NOT NULL DEFAULT '',
  lease_owner VARCHAR(128) NULL,
  lease_until DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  deadline_at DATETIME(6) NOT NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  UNIQUE KEY uk_sb_task_idempotency (origin_installation_id,actor_id,task_type,idempotency_key),
  INDEX idx_sb_task_claim (status,lease_until,created_at),
  CONSTRAINT ck_sb_task_type CHECK (task_type IN ('backup','drill','preflight','artifact_verify')),
  CONSTRAINT ck_sb_task_status CHECK (status IN ('pending','preparing','ready_for_capture','acquiring_gate','capturing','validating','publishing','finalization_unknown','preflighting','provisioning','restoring','normalizing','verifying','running','canceling','succeeded','failed','canceled')),
  CONSTRAINT ck_sb_task_terminal CHECK ((status IN ('succeeded','failed','canceled') AND finished_at IS NOT NULL AND can_cancel=0) OR (status NOT IN ('succeeded','failed','canceled') AND finished_at IS NULL)),
  CONSTRAINT ck_sb_task_commit CHECK (commit_started=0 OR can_cancel=0),
  CONSTRAINT ck_sb_task_eligible CHECK (acceptance_eligible=0 OR (status='succeeded' AND artifact_registered=1)),
  CONSTRAINT ck_sb_task_unknown CHECK (status<>'finalization_unknown' OR (task_type='backup' AND commit_started=1 AND can_cancel=0 AND failure_category='none' AND attention_reason='external_result_unknown')),
  CONSTRAINT ck_sb_task_request CHECK (JSON_VALID(request_json) AND JSON_VALID(config_snapshot))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_events (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  task_id VARCHAR(40) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  details_json TEXT NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  INDEX idx_sb_events_task (task_id,id),
  CONSTRAINT ck_sb_event_json CHECK (JSON_VALID(details_json))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_gate (
  scope VARCHAR(128) PRIMARY KEY,
  phase VARCHAR(16) NOT NULL DEFAULT 'idle',
  generation BIGINT UNSIGNED NOT NULL DEFAULT 0,
  fencing_token BIGINT UNSIGNED NOT NULL DEFAULT 0,
  owner VARCHAR(128) NULL,
  acquire_deadline DATETIME(6) NULL,
  hold_deadline DATETIME(6) NULL,
  registry_hash CHAR(64) NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  CONSTRAINT ck_sb_gate_phase CHECK (phase IN ('idle','acquiring','held','releasing'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_mutation_leases (
  public_id VARCHAR(40) PRIMARY KEY,
  scope VARCHAR(128) NOT NULL,
  generation BIGINT UNSIGNED NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  released_at DATETIME(6) NULL,
  INDEX idx_sb_mutation_scope (scope,released_at,generation),
  FOREIGN KEY (scope) REFERENCES system_backup_gate(scope)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_participants (
  scope VARCHAR(128) NOT NULL,
  participant_id VARCHAR(128) NOT NULL,
  generation BIGINT UNSIGNED NOT NULL,
  state VARCHAR(16) NOT NULL,
  resume_result VARCHAR(16) NOT NULL DEFAULT 'none',
  checkpoint_hash CHAR(64) NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (scope,participant_id),
  CONSTRAINT ck_sb_participant_state CHECK (state IN ('running','pausing','paused','resuming','error')),
  CONSTRAINT ck_sb_participant_resume CHECK (resume_result IN ('none','succeeded','forced','failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_restore_evidence (
  run_id VARCHAR(40) NOT NULL,
  entity_type VARCHAR(64) NOT NULL,
  entity_key_hash CHAR(64) NOT NULL,
  source_hash CHAR(64) NOT NULL,
  encrypted_payload LONGBLOB NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (run_id,entity_type,entity_key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_quarantine (
  target_identity_hash CHAR(64) NOT NULL,
  entity_type VARCHAR(64) NOT NULL,
  entity_key_hash CHAR(64) NOT NULL,
  run_id VARCHAR(40) NOT NULL,
  reason VARCHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (target_identity_hash,entity_type,entity_key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS system_backup_operations (
  public_id VARCHAR(40) PRIMARY KEY,
  actor_id INT NOT NULL,
  endpoint VARCHAR(160) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  request_hash CHAR(64) NOT NULL,
  status VARCHAR(24) NOT NULL,
  response_json LONGTEXT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uk_sb_operation_idempotency (actor_id,endpoint,idempotency_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
