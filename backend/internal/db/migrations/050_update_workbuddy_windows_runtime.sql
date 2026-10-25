UPDATE system_image_settings
SET image = 'ghcr.io/yuan-lab-llm/agentsruntime/windows-vm-workbuddy:latest',
    runtime_type = 'desktop',
    display_name = 'Workbuddy Pro'
WHERE instance_type = 'workbuddy'
  AND image = 'ghcr.io/yuan-lab-llm/agentsruntime/workbuddy-linux:latest';
