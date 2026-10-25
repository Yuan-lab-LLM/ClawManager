ALTER TABLE instances
MODIFY COLUMN type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes', 'workbuddy', 'opencode', 'deepseek-harness', 'codex', 'claude-code') DEFAULT 'ubuntu';

INSERT INTO system_image_settings (instance_type, runtime_type, display_name, image, is_enabled)
SELECT 'codex', 'desktop', 'Codex Pro', 'ghcr.io/yuan-lab-llm/agentsruntime/codex:latest', TRUE
WHERE NOT EXISTS (
  SELECT 1 FROM system_image_settings
  WHERE instance_type = 'codex' AND runtime_type = 'desktop'
);

INSERT INTO system_image_settings (instance_type, runtime_type, display_name, image, is_enabled)
SELECT 'claude-code', 'desktop', 'Claude Code Pro', 'ghcr.io/yuan-lab-llm/agentsruntime/claude-code:latest', TRUE
WHERE NOT EXISTS (
  SELECT 1 FROM system_image_settings
  WHERE instance_type = 'claude-code' AND runtime_type = 'desktop'
);
