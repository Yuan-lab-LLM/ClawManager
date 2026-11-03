-- Historical duplicates are reported by the migration runner before this DDL.
-- Preserve the column's existing collation and all user/identity data.
SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'uk_users_username') = 0,
  'ALTER TABLE users ADD UNIQUE KEY uk_users_username (username)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
