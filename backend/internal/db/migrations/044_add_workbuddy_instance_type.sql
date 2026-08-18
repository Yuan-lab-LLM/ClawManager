ALTER TABLE instances
MODIFY COLUMN type ENUM('openclaw', 'ubuntu', 'debian', 'centos', 'custom', 'webtop', 'hermes', 'workbuddy') DEFAULT 'ubuntu';

UPDATE system_image_settings
SET instance_type = 'workbuddy',
    runtime_type = 'desktop',
    display_name = 'Workbuddy Pro'
WHERE instance_type = 'custom'
  AND LOWER(TRIM(display_name)) = 'workbuddy';
