-- OpenClaw instances: change PVC mount from /config to /home/node/.openclaw
-- so that the workspace (memory, config, plugins) survives pod restarts.
UPDATE instances
SET mount_path = '/home/node/.openclaw'
WHERE type = 'openclaw'
  AND mount_path IN ('/config', '/home/user/data');

