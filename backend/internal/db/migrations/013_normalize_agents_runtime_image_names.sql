UPDATE system_image_settings
SET image = 'ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest'
WHERE image = 'ghcr.io/Yuan-lab-LLM/AgentsRuntime/openclaw:latest';

UPDATE system_image_settings
SET image = 'ghcr.io/yuan-lab-llm/agentsruntime/hermes:latest'
WHERE image = 'ghcr.io/Yuan-lab-LLM/AgentsRuntime/hermes:latest';
