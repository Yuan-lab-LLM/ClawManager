UPDATE system_image_settings
SET image = 'ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest', updated_at = UTC_TIMESTAMP(6)
WHERE instance_type = 'openclaw'
  AND runtime_type = 'desktop'
  AND image IN (
    'ghcr.io/yuan-lab-llm/agentsruntime/openclaw:2026.8.1'
  );

UPDATE system_image_settings
SET image = 'ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest', updated_at = UTC_TIMESTAMP(6)
WHERE instance_type = 'openclaw'
  AND runtime_type = 'gateway'
  AND image IN (
    'ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:2026.8.1'
  );
