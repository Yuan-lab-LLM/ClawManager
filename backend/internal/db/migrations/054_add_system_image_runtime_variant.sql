SET @system_image_runtime_variant_column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'system_image_settings'
    AND COLUMN_NAME = 'runtime_variant'
);
SET @system_image_runtime_variant_column_sql = IF(
  @system_image_runtime_variant_column_exists = 0,
  'ALTER TABLE system_image_settings ADD COLUMN runtime_variant VARCHAR(32) NOT NULL DEFAULT '''' AFTER runtime_type',
  'SELECT 1'
);
PREPARE system_image_runtime_variant_column_stmt FROM @system_image_runtime_variant_column_sql;
EXECUTE system_image_runtime_variant_column_stmt;
DEALLOCATE PREPARE system_image_runtime_variant_column_stmt;

UPDATE system_image_settings
SET runtime_variant = CASE
  WHEN LOWER(image) LIKE '%workbuddy-linux%' THEN 'linux'
  WHEN LOWER(image) LIKE '%windows-vm-workbuddy%' OR LOWER(image) LIKE '%dockur/windows%' THEN 'windows'
  WHEN LOWER(image) LIKE '%windows-vm-codex%' THEN 'windows'
  WHEN instance_type = 'codex' AND LOWER(image) LIKE '%agentsruntime/codex%' THEN 'linux'
  ELSE runtime_variant
END
WHERE runtime_type = 'desktop'
  AND instance_type IN ('workbuddy', 'codex')
  AND runtime_variant = '';

UPDATE instances
SET runtime_variant = CASE
  WHEN LOWER(COALESCE(image_registry, '')) LIKE '%windows-vm-codex%' OR mount_path = '/storage' THEN 'windows'
  WHEN LOWER(COALESCE(image_registry, '')) LIKE '%agentsruntime/codex%' OR mount_path = '/config' THEN 'linux'
  ELSE runtime_variant
END
WHERE type = 'codex'
  AND runtime_variant = '';
