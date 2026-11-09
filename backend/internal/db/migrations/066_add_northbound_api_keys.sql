-- Additive only: Gateway keeps its existing table-level SELECT grant.
SET @nb_keys_sql = IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_caller_policies' AND COLUMN_NAME='api_keys_json')=0,
 'ALTER TABLE northbound_caller_policies ADD COLUMN api_keys_json JSON NULL', 'SELECT 1');
PREPARE nb_keys_stmt FROM @nb_keys_sql;
EXECUTE nb_keys_stmt;
DEALLOCATE PREPARE nb_keys_stmt;
-- Core-only provenance, no additional Gateway grants. Preserve the original
-- route scope when an operation is normalized between API domains.
SET @nb_keys_sql = IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND COLUMN_NAME='api_key_id')=0,
 'ALTER TABLE northbound_operations ADD COLUMN api_key_id VARCHAR(64) NULL', 'SELECT 1');
PREPARE nb_keys_stmt FROM @nb_keys_sql;
EXECUTE nb_keys_stmt;
DEALLOCATE PREPARE nb_keys_stmt;
SET @nb_keys_sql = IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_operations' AND COLUMN_NAME='auth_scope')=0,
 'ALTER TABLE northbound_operations ADD COLUMN auth_scope VARCHAR(64) NULL', 'SELECT 1');
PREPARE nb_keys_stmt FROM @nb_keys_sql;
EXECUTE nb_keys_stmt;
DEALLOCATE PREPARE nb_keys_stmt;
SET @nb_keys_sql = IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='northbound_caller_policies' AND COLUMN_NAME='api_keys_version')=0,
 'ALTER TABLE northbound_caller_policies ADD COLUMN api_keys_version BIGINT NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE nb_keys_stmt FROM @nb_keys_sql;
EXECUTE nb_keys_stmt;
DEALLOCATE PREPARE nb_keys_stmt;
