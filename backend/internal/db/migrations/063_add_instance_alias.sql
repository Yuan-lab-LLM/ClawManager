SET @instance_alias_column_exists = (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'instances'
    AND COLUMN_NAME = 'alias'
);
SET @instance_alias_column_sql = IF(
  @instance_alias_column_exists = 0,
  'ALTER TABLE instances ADD COLUMN alias VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NULL AFTER name',
  'SELECT 1'
);
PREPARE instance_alias_column_stmt FROM @instance_alias_column_sql;
EXECUTE instance_alias_column_stmt;
DEALLOCATE PREPARE instance_alias_column_stmt;
