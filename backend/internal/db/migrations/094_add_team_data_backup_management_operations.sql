ALTER TABLE system_team_data_backup_jobs DROP CHECK chk_team_data_backup_operation;
ALTER TABLE system_team_data_backup_jobs ADD CONSTRAINT chk_team_data_backup_operation CHECK (operation IN ('backup','restore','cleanup','verify','delete'));
