ALTER TABLE system_backup_attempts ADD COLUMN request_json LONGTEXT NULL;
ALTER TABLE system_backup_attempts ADD CONSTRAINT ck_sb_attempt_request CHECK(request_json IS NULL OR JSON_VALID(request_json));
