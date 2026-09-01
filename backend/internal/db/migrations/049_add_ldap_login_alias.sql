SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'login_alias') = 0,
  'ALTER TABLE users ADD COLUMN login_alias VARCHAR(255) NULL AFTER auth_provider',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE users AS u
JOIN (
  SELECT ldap_usernames.username
  FROM (
    SELECT username
    FROM users
    WHERE auth_provider = 'ldap'
      AND external_id IS NOT NULL
    GROUP BY username
    HAVING COUNT(*) = 1
  ) AS ldap_usernames
) AS unique_ldap_usernames ON unique_ldap_usernames.username = u.username
SET u.login_alias = CONCAT('ldap_', LOWER(u.username))
WHERE u.auth_provider = 'ldap'
  AND u.external_id IS NOT NULL
  AND COALESCE(TRIM(u.login_alias), '') = '';

-- LDAP rows with a duplicated uid intentionally remain NULL here. They are
-- marked as pending alias completion by the LDAP import preview and receive
-- deterministic OU-qualified aliases when they are re-imported.

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'username') > 0,
  'ALTER TABLE users DROP INDEX username',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'local_username_key') = 0,
  'ALTER TABLE users ADD COLUMN local_username_key VARCHAR(255) GENERATED ALWAYS AS (CASE WHEN auth_provider = ''local'' THEN username ELSE NULL END) STORED',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'uk_users_local_username') = 0,
  'ALTER TABLE users ADD UNIQUE KEY uk_users_local_username (local_username_key)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'uk_users_provider_login_alias') = 0,
  'ALTER TABLE users ADD UNIQUE KEY uk_users_provider_login_alias (auth_provider, login_alias)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @stmt = IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'uk_users_provider_external_id') = 0,
  'ALTER TABLE users ADD UNIQUE KEY uk_users_provider_external_id (auth_provider, external_id)',
  'SELECT 1'
);
PREPARE stmt FROM @stmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
