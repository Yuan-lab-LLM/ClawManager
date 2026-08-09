ALTER TABLE skills
  ADD COLUMN summary_status VARCHAR(32) NOT NULL DEFAULT 'idle' AFTER description,
  ADD COLUMN summary_error TEXT NULL AFTER summary_status;

CREATE INDEX idx_skills_summary_status ON skills (summary_status);
