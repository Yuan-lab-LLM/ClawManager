ALTER TABLE team_events
  ADD COLUMN event_id VARCHAR(128) NULL AFTER id,
  ADD COLUMN completion_id VARCHAR(255) NULL AFTER event_id,
  ADD COLUMN sequence_no BIGINT NOT NULL DEFAULT 0 AFTER completion_id;

UPDATE team_events SET sequence_no = id WHERE sequence_no = 0;

ALTER TABLE team_events
  ADD UNIQUE KEY uk_team_events_event_id (team_id, event_id),
  ADD UNIQUE KEY uk_team_events_completion_id (team_id, completion_id),
  ADD INDEX idx_team_events_team_sequence (team_id, sequence_no);

CREATE TABLE IF NOT EXISTS team_work_items (
  id INT AUTO_INCREMENT PRIMARY KEY,
  team_id INT NOT NULL,
  root_task_id INT NOT NULL,
  work_id VARCHAR(255) NOT NULL,
  owner_member_id INT NULL,
  title VARCHAR(500) NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'pending',
  depends_on_json LONGTEXT NULL,
  result_json LONGTEXT NULL,
  artifact_refs_json LONGTEXT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  started_at TIMESTAMP NULL,
  finished_at TIMESTAMP NULL,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
  FOREIGN KEY (root_task_id) REFERENCES team_tasks(id) ON DELETE CASCADE,
  FOREIGN KEY (owner_member_id) REFERENCES team_members(id) ON DELETE SET NULL,
  UNIQUE KEY uk_team_work_items_work (team_id, root_task_id, work_id),
  INDEX idx_team_work_items_root_status (root_task_id, status),
  INDEX idx_team_work_items_owner_status (owner_member_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
