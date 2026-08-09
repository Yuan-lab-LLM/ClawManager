-- Pairs with 042_add_skill_summary_status.sql to remove LLM summary status columns.
-- Migration numbers may overlap other 042/043 files; schema_migrations tracks by filename.

DROP INDEX idx_skills_summary_status ON skills;

ALTER TABLE skills
  DROP COLUMN summary_error,
  DROP COLUMN summary_status;
