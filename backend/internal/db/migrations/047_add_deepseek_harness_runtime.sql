ALTER TABLE instances
MODIFY COLUMN type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes', 'workbuddy', 'deepseek-harness') DEFAULT 'ubuntu';

INSERT INTO system_image_settings (instance_type, runtime_type, display_name, image, is_enabled)
SELECT
  'deepseek-harness',
  'desktop',
  'DeepSeek Harness Pro',
  'ghcr.io/yuan-lab-llm/agentsruntime/deepseek-harness:latest',
  TRUE
WHERE NOT EXISTS (
  SELECT 1
  FROM system_image_settings
  WHERE instance_type = 'deepseek-harness'
    AND runtime_type = 'desktop'
);

INSERT INTO system_image_settings (instance_type, runtime_type, display_name, image, is_enabled)
SELECT
  'deepseek-harness',
  'gateway',
  'DeepSeek Harness Lite',
  'ghcr.io/yuan-lab-llm/agentsruntime/deepseek-harness-lite:latest',
  TRUE
WHERE NOT EXISTS (
  SELECT 1
  FROM system_image_settings
  WHERE instance_type = 'deepseek-harness'
    AND runtime_type IN ('shell', 'gateway')
);
