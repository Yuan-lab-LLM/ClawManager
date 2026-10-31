-- Recovery-side control metadata only; never part of a Team data archive.
ALTER TABLE system_team_data_backup_jobs ADD COLUMN provisioning_json JSON NULL;
