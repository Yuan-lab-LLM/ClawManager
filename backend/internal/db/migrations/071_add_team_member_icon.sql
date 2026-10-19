-- Presentation only; no change to Runtime configuration or existing member state.
-- Also tolerate a restart after the DDL succeeded but before it was recorded.
SET @team_icon_ddl = IF(
  EXISTS(SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'team_members' AND COLUMN_NAME = 'icon_key'),
  'SELECT 1',
  'ALTER TABLE team_members ADD COLUMN icon_key VARCHAR(32) NULL'
);
PREPARE team_icon_stmt FROM @team_icon_ddl;
EXECUTE team_icon_stmt;
DEALLOCATE PREPARE team_icon_stmt;
