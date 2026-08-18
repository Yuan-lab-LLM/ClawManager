ALTER TABLE instances
MODIFY COLUMN type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes', 'opencode') DEFAULT 'ubuntu';

INSERT INTO system_image_settings (instance_type, runtime_type, display_name, image, is_enabled)
SELECT
  'opencode',
  'desktop',
  'OpenCode Pro',
  'ghcr.io/yuan-lab-llm/agentsruntime/opencode:latest',
  TRUE
WHERE NOT EXISTS (
  SELECT 1
  FROM system_image_settings
  WHERE instance_type = 'opencode'
    AND runtime_type = 'desktop'
);

INSERT INTO system_image_settings (instance_type, runtime_type, display_name, image, is_enabled)
SELECT
  'opencode',
  'gateway',
  'OpenCode Lite',
  'ghcr.io/yuan-lab-llm/agentsruntime/opencode-lite:latest',
  TRUE
WHERE NOT EXISTS (
  SELECT 1
  FROM system_image_settings
  WHERE instance_type = 'opencode'
    AND runtime_type = 'gateway'
);
