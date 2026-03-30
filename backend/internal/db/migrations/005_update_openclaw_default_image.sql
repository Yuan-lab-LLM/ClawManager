UPDATE system_image_settings
SET image = 'ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest'
WHERE instance_type = 'openclaw'
  AND image = 'ericpearlee/openclaw:v2026.3.24';
