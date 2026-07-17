ALTER TABLE skills
  ADD COLUMN visibility ENUM('private', 'public') NOT NULL DEFAULT 'private' AFTER status,
  ADD COLUMN published_at TIMESTAMP NULL AFTER visibility,
  ADD COLUMN published_by INT NULL AFTER published_at,
  ADD INDEX idx_skills_visibility (visibility, status, source_type);

ALTER TABLE skills
  ADD CONSTRAINT fk_skills_published_by FOREIGN KEY (published_by) REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS skill_hub_tags (
  id INT AUTO_INCREMENT PRIMARY KEY,
  tag_key VARCHAR(64) NOT NULL,
  name VARCHAR(120) NOT NULL,
  description VARCHAR(255) NULL,
  sort_order INT NOT NULL DEFAULT 0,
  admin_only BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_skill_hub_tags_tag_key (tag_key),
  INDEX idx_skill_hub_tags_sort (sort_order, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS skill_hub_tag_assignments (
  id INT AUTO_INCREMENT PRIMARY KEY,
  skill_id INT NOT NULL,
  tag_id INT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (skill_id) REFERENCES skills(id) ON DELETE CASCADE,
  FOREIGN KEY (tag_id) REFERENCES skill_hub_tags(id) ON DELETE CASCADE,
  UNIQUE KEY uk_skill_hub_tag_assignments (skill_id, tag_id),
  INDEX idx_skill_hub_tag_assignments_tag (tag_id, skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO skill_hub_tags (tag_key, name, description, sort_order, admin_only) VALUES
  ('productivity', 'Productivity', 'Efficiency and workflow skills', 10, FALSE),
  ('coding', 'Coding', 'Software development skills', 20, FALSE),
  ('browser', 'Browser', 'Browser automation skills', 30, FALSE),
  ('data', 'Data', 'Data processing and analytics skills', 40, FALSE),
  ('communication', 'Communication', 'Messaging and collaboration skills', 50, FALSE),
  ('automation', 'Automation', 'Task automation skills', 60, FALSE),
  ('research', 'Research', 'Research and information gathering skills', 70, FALSE),
  ('community', 'Community', 'Community shared skills', 80, FALSE),
  ('admin-curated', 'Admin Curated', 'Curated by platform administrators', 90, TRUE),
  ('featured', 'Featured', 'Featured on the Skill Hub', 100, TRUE);

UPDATE skills SET visibility = 'private';
