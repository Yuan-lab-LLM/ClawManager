CREATE TABLE IF NOT EXISTS team_event_outbox (
  id INT AUTO_INCREMENT PRIMARY KEY,
  team_id INT NOT NULL,
  source_event_id VARCHAR(255) NOT NULL,
  destination VARCHAR(255) NOT NULL,
  message_id VARCHAR(255) NOT NULL,
  payload_json LONGTEXT NOT NULL,
  status VARCHAR(30) NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  available_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_error TEXT NULL,
  delivered_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
  UNIQUE KEY uk_team_event_outbox_message (team_id, destination, message_id),
  INDEX idx_team_event_outbox_pending (status, available_at),
  INDEX idx_team_event_outbox_team (team_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
