ALTER TABLE team_tasks
  ADD COLUMN workflow_state VARCHAR(40) NOT NULL DEFAULT 'open' AFTER status,
  ADD COLUMN plan_version BIGINT NOT NULL DEFAULT 0 AFTER workflow_state,
  ADD COLUMN ledger_version BIGINT NOT NULL DEFAULT 0 AFTER plan_version,
  ADD COLUMN current_phase_id VARCHAR(255) NULL AFTER ledger_version,
  ADD COLUMN accepted_completion_id VARCHAR(255) NULL AFTER current_phase_id,
  ADD UNIQUE KEY uk_team_tasks_accepted_completion (team_id, accepted_completion_id),
  ADD INDEX idx_team_tasks_workflow (team_id, workflow_state, status);

ALTER TABLE team_work_items
  ADD COLUMN assignment_id VARCHAR(255) NULL AFTER work_id,
  ADD COLUMN canonical_work_id VARCHAR(255) NULL AFTER assignment_id,
  ADD COLUMN phase_id VARCHAR(255) NULL AFTER canonical_work_id,
  ADD COLUMN revision INT NOT NULL DEFAULT 1 AFTER phase_id,
  ADD COLUMN required_for_root BOOLEAN NOT NULL DEFAULT TRUE AFTER revision,
  ADD COLUMN superseded_by VARCHAR(255) NULL AFTER required_for_root,
  ADD COLUMN review_required BOOLEAN NOT NULL DEFAULT FALSE AFTER superseded_by,
  ADD COLUMN validated_revision INT NULL AFTER review_required,
  ADD INDEX idx_team_work_items_assignment (root_task_id, assignment_id, revision),
  ADD INDEX idx_team_work_items_phase (root_task_id, phase_id, status);

UPDATE team_work_items
SET assignment_id = work_id,
    canonical_work_id = work_id,
    phase_id = 'legacy'
WHERE assignment_id IS NULL OR canonical_work_id IS NULL OR phase_id IS NULL;

CREATE TABLE IF NOT EXISTS team_workflow_phases (
  id INT AUTO_INCREMENT PRIMARY KEY,
  team_id INT NOT NULL,
  root_task_id INT NOT NULL,
  phase_id VARCHAR(255) NOT NULL,
  plan_version BIGINT NOT NULL DEFAULT 1,
  sequence_no INT NOT NULL DEFAULT 0,
  status VARCHAR(40) NOT NULL DEFAULT 'planned',
  required_for_root BOOLEAN NOT NULL DEFAULT TRUE,
  decision_required BOOLEAN NOT NULL DEFAULT FALSE,
  depends_on_json LONGTEXT NULL,
  next_phase_id VARCHAR(255) NULL,
  completion_policy VARCHAR(80) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
  FOREIGN KEY (root_task_id) REFERENCES team_tasks(id) ON DELETE CASCADE,
  UNIQUE KEY uk_team_workflow_phase (root_task_id, phase_id, plan_version),
  INDEX idx_team_workflow_phase_state (root_task_id, plan_version, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
