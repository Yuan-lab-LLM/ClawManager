SET @instance_environment_overrides_column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'instances'
    AND COLUMN_NAME = 'environment_overrides_json'
);
SET @instance_environment_overrides_column_sql = IF(
  @instance_environment_overrides_column_exists = 0,
  'ALTER TABLE instances ADD COLUMN environment_overrides_json LONGTEXT NULL AFTER image_tag',
  'SELECT 1'
);
PREPARE instance_environment_overrides_column_stmt FROM @instance_environment_overrides_column_sql;
EXECUTE instance_environment_overrides_column_stmt;
DEALLOCATE PREPARE instance_environment_overrides_column_stmt;
