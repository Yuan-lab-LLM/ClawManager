SET @instance_pvc_name_column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'instances'
    AND COLUMN_NAME = 'pvc_name'
);
SET @instance_pvc_name_column_sql = IF(
  @instance_pvc_name_column_exists = 0,
  'ALTER TABLE instances ADD COLUMN pvc_name VARCHAR(253) NULL AFTER storage_class',
  'SELECT 1'
);
PREPARE instance_pvc_name_column_stmt FROM @instance_pvc_name_column_sql;
EXECUTE instance_pvc_name_column_stmt;
DEALLOCATE PREPARE instance_pvc_name_column_stmt;
