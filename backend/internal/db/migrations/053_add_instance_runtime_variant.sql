SET @instance_runtime_variant_column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'instances'
    AND COLUMN_NAME = 'runtime_variant'
);
SET @instance_runtime_variant_column_sql = IF(
  @instance_runtime_variant_column_exists = 0,
  'ALTER TABLE instances ADD COLUMN runtime_variant VARCHAR(32) NOT NULL DEFAULT '''' AFTER runtime_type',
  'SELECT 1'
);
PREPARE instance_runtime_variant_column_stmt FROM @instance_runtime_variant_column_sql;
EXECUTE instance_runtime_variant_column_stmt;
DEALLOCATE PREPARE instance_runtime_variant_column_stmt;

UPDATE instances
SET runtime_variant = 'linux'
WHERE type = 'workbuddy'
  AND (
    mount_path = '/config'
    OR LOWER(COALESCE(image_registry, '')) LIKE '%workbuddy-linux%'
  );

UPDATE instances
SET runtime_variant = 'windows'
WHERE type = 'workbuddy'
  AND runtime_variant = ''
  AND (
    mount_path = '/storage'
    OR LOWER(COALESCE(image_registry, '')) LIKE '%windows-vm-workbuddy%'
    OR pvc_name IS NOT NULL
  );
