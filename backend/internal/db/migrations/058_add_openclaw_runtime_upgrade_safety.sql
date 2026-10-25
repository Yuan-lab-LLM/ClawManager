ALTER TABLE runtime_pods
  ADD COLUMN openclaw_version VARCHAR(64) NULL AFTER image_ref,
  ADD COLUMN agent_protocol_version VARCHAR(32) NULL AFTER openclaw_version,
  ADD COLUMN team_plugin_version VARCHAR(64) NULL AFTER agent_protocol_version,
  ADD COLUMN session_store VARCHAR(32) NULL AFTER team_plugin_version,
  ADD COLUMN image_digest VARCHAR(255) NULL AFTER session_store,
  ADD COLUMN capabilities_json JSON NULL AFTER image_digest;

ALTER TABLE runtime_rollouts
  ADD COLUMN phase VARCHAR(32) NOT NULL DEFAULT 'requested' AFTER status,
  ADD COLUMN source_images_json JSON NULL AFTER target_image_ref,
  ADD COLUMN target_image_digest VARCHAR(255) NULL AFTER source_images_json,
  ADD COLUMN preflight_id VARCHAR(64) NULL AFTER target_image_digest,
  ADD COLUMN plan_fingerprint VARCHAR(64) NULL AFTER preflight_id,
  ADD COLUMN preflight_json JSON NULL AFTER plan_fingerprint,
  ADD COLUMN required_capabilities_json JSON NULL AFTER preflight_json,
  ADD COLUMN auto_rollback BOOLEAN NOT NULL DEFAULT TRUE AFTER required_capabilities_json,
  ADD COLUMN rollback_status VARCHAR(32) NULL AFTER auto_rollback,
  ADD COLUMN rollback_error TEXT NULL AFTER rollback_status;

CREATE UNIQUE INDEX uk_runtime_rollouts_preflight_id ON runtime_rollouts (preflight_id);

CREATE TABLE IF NOT EXISTS runtime_upgrade_items (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  rollout_id BIGINT NOT NULL,
  team_id INT NULL,
  team_member_id INT NULL,
  instance_id INT NOT NULL,
  runtime_pod_id BIGINT NULL,
  member_order INT NOT NULL DEFAULT 0,
  is_team_leader BOOLEAN NOT NULL DEFAULT FALSE,
  source_runtime_version VARCHAR(64) NULL,
  target_runtime_version VARCHAR(64) NULL,
  state VARCHAR(32) NOT NULL DEFAULT 'pending',
  workspace_manifest_sha256 VARCHAR(64) NULL,
  snapshot_ref VARCHAR(1024) NULL,
  preflight_json JSON NULL,
  postflight_json JSON NULL,
  error_message TEXT NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uk_runtime_upgrade_item_rollout_instance (rollout_id, instance_id),
  KEY idx_runtime_upgrade_item_team_order (rollout_id, team_id, member_order),
  KEY idx_runtime_upgrade_item_state (rollout_id, state),
  CONSTRAINT fk_runtime_upgrade_item_rollout FOREIGN KEY (rollout_id) REFERENCES runtime_rollouts(id) ON DELETE CASCADE,
  CONSTRAINT fk_runtime_upgrade_item_instance FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE RESTRICT,
  CONSTRAINT fk_runtime_upgrade_item_team FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE RESTRICT,
  CONSTRAINT fk_runtime_upgrade_item_member FOREIGN KEY (team_member_id) REFERENCES team_members(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS runtime_upgrade_audits (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  rollout_id BIGINT NULL,
  actor_user_id INT NULL,
  action VARCHAR(64) NOT NULL,
  phase VARCHAR(32) NOT NULL,
  outcome VARCHAR(32) NOT NULL,
  detail_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  KEY idx_runtime_upgrade_audit_rollout_created (rollout_id, created_at),
  CONSTRAINT fk_runtime_upgrade_audit_rollout FOREIGN KEY (rollout_id) REFERENCES runtime_rollouts(id) ON DELETE SET NULL,
  CONSTRAINT fk_runtime_upgrade_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
