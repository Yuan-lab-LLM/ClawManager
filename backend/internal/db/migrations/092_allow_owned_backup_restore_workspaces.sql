ALTER TABLE system_backup_stage_resources DROP CHECK ck_sb_stage_phase,
 ADD CONSTRAINT ck_sb_stage_phase CHECK(phase IN ('capture','secret_capture','restore'));
